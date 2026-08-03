package harness

import (
	"context"
	"fmt"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
	"github.com/sonnes/pi-go/pkg/session"
)

// Agent builds an agent: it overlays the caller's options onto the
// harness baseline, resolves every artifact fresh, compiles them into a
// system prompt and a tool list, and binds the result to a durable
// session.
//
// What comes back is a plain [durable.Agent] — the harness compiles the
// configuration and steps out of the way. Seeding rides down as
// [durable.WithMiddleware], so there is nothing left for a wrapper to
// override, and every durable verb (Fork included) behaves the same
// whether the agent was minted here or by [durable.New].
//
// The artifacts behind it are a snapshot taken when it was built, so
// its system prompt and tool list are stable for its lifetime. Edits to
// the underlying definitions show up in the next build.
//
// Options are the same flat currency [New] takes, and every layer is
// accepted here: [durable.WithSessionID] names the session to create or
// resume, [durable.WithMiddleware] wraps its runs, and any harness
// option overlays the baseline for this build alone. Scalars override,
// tools append, and a resolver is an additional source layered above
// the baseline's — it wins the names it claims and leaves the rest
// standing. Passing a project's resolvers and working directory here is
// what lets one harness serve many repositories.
//
// A session with no history is seeded from the configured
// [prompt.Seeder] on its first run.
func (h *Harness[T]) Agent(ctx context.Context, opts ...agent.Option) (*durable.Agent[T], error) {
	b, err := h.overlay(opts)
	if err != nil {
		return nil, err
	}

	res, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}

	tools := b.compileTools(res)
	env := b.env(b.lm.Model(), tools, res)

	sys, err := b.system(ctx, env)
	if err != nil {
		return nil, err
	}

	// The seed is built before the session exists, because the
	// middleware that injects it has to be registered at durable.New.
	// Whether it is needed is still decided per run, against the leaf
	// pointer — so a resumed session simply never uses what was built
	// here. The cost is a seeder call whose result may be discarded;
	// the gain is that a seeder failure is a build error the caller can
	// act on, rather than a failed stream mid-run.
	seed, err := b.seed(ctx, env)
	if err != nil {
		return nil, err
	}
	inject := &seedInjector{entries: seed}

	a, err := durable.New[T](
		ctx,
		b.lm,
		h.agentOpts(sys, tools, opts, durable.WithMiddleware(inject.middleware))...,
	)
	if err != nil {
		return nil, err
	}
	inject.leafID = a.LeafID

	return a, nil
}

// Env compiles a build and returns the [prompt.Env] it would hand its
// builders — the resolved agent definitions, skills, and instructions,
// the tool list with synthesized tools included, the model, and the
// working directory. No session is created and no seeder runs.
//
// It is the read-only view of a build: a UI listing the skills a
// session would see, or a debugging session asking what a given overlay
// resolves to, gets the answer without minting an agent. Options
// overlay the baseline exactly as they do in [Harness.Agent].
func (h *Harness[T]) Env(ctx context.Context, opts ...agent.Option) (*prompt.Env, error) {
	b, err := h.overlay(opts)
	if err != nil {
		return nil, err
	}

	res, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}

	tools := b.compileTools(res)
	return b.env(b.lm.Model(), tools, res), nil
}

// agentOpts assembles the option list for the durable agent: the
// harness's own options, then the caller's, then the compiled prompt
// and tools — which go last so the harness's compilation wins over
// anything the caller set directly.
//
// The seed option goes last of all, so the seed middleware is
// registered after any the caller passed and ends up innermost: every
// other middleware sees the run before the seed entries are added.
func (h *Harness[T]) agentOpts(
	sys string,
	tools []ai.Tool,
	callerOpts []agent.Option,
	seed agent.Option,
) []agent.Option {
	out := make([]agent.Option, 0, len(h.opts)+len(callerOpts)+3)
	out = append(out, h.opts...)
	out = append(out, callerOpts...)
	out = append(out,
		agent.WithSystemPrompt(sys),
		agent.WithTools(tools...),
		seed,
	)
	return out
}

// env assembles the snapshot handed to the builders.
func (b *build) env(
	model ai.Model,
	tools []ai.Tool,
	res *resolution,
) *prompt.Env {
	infos := make([]ai.ToolInfo, len(tools))
	for i, t := range tools {
		infos[i] = t.Info()
	}
	return &prompt.Env{
		Model:        model,
		Tools:        infos,
		Agents:       res.agents,
		Skills:       res.skills,
		Instructions: res.instructions,
		WorkDir:      b.workDir,
	}
}

// system runs the configured [prompt.Builder]. compile always sets one,
// so a build reaches the nil branch — and its agent gets no system
// prompt — only when it was assembled by hand.
func (b *build) system(ctx context.Context, env *prompt.Env) (string, error) {
	if b.builder == nil {
		return "", nil
	}
	sys, err := b.builder(ctx, env)
	if err != nil {
		return "", fmt.Errorf("harness: build system prompt: %w", err)
	}
	return sys, nil
}

// seed runs the configured [prompt.Seeder] and normalizes its entries:
// message entries that are not ephemeral are marked meta, so seeded
// context is model-visible, transcript-hidden, and durable whether or
// not the seeder remembered to say so.
func (b *build) seed(ctx context.Context, env *prompt.Env) ([]session.Entry, error) {
	if b.seeder == nil {
		return nil, nil
	}
	entries, err := b.seeder(ctx, env)
	if err != nil {
		return nil, fmt.Errorf("harness: build seed entries: %w", err)
	}
	out := make([]session.Entry, 0, len(entries))
	for _, e := range entries {
		if me, ok := session.AsMessageEntry(e); ok && !me.Ephemeral {
			me.Meta = true
			out = append(out, me)
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// seedInjector prepends a build's seed entries to a run while the
// session has no persisted history.
//
// The check is the leaf pointer, not a fired-once flag: the durable run
// persists its input before anything else, so a run that failed before
// persisting leaves the leaf empty and the next run re-seeds. Once any
// run persists, the seed is in history and never repeats — which is
// also why a resumed session never injects.
//
// leafID is wired after [durable.New] returns, because the middleware
// must be registered before the agent it asks about exists. That is
// safe: the middleware body runs only from [durable.Agent.Run], which
// no caller can reach until New has handed the agent back.
//
// Known edge: a fork re-instantiates this middleware but the closure
// still reads the parent's leaf, so a child forked from a never-run
// session whose parent then runs first will not inject. Forking a
// session with no history is already a degenerate case, and every other
// ordering is correct; closing it properly needs the middleware to know
// which agent it is wrapping.
type seedInjector struct {
	entries []session.Entry
	leafID  func() string
}

func (s *seedInjector) middleware(next durable.Runner) durable.Runner {
	return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
		fresh := s.leafID != nil && s.leafID() == ""
		if len(s.entries) > 0 && fresh {
			entries = append(append([]session.Entry{}, s.entries...), entries...)
		}
		return next(ctx, entries...)
	}
}
