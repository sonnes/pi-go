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

// Agent builds an agent. It overlays the options of the caller onto the
// harness baseline. It then resolves every artifact fresh, compiles
// them into a system prompt and a tool list, and binds the result to a
// durable session.
//
// The result is a plain [durable.Agent]. The harness compiles the
// configuration and then steps out of the way. Seeding rides down as
// [durable.WithMiddleware], so a wrapper has nothing left to override.
// Every durable verb, Fork included, behaves the same for an agent
// minted here and for one from [durable.New].
//
// The artifacts behind the agent are a snapshot from build time. The
// system prompt and the tool list are therefore stable for the lifetime
// of the agent. An edit to the underlying definitions appears in the
// next build.
//
// Options are the same flat currency [New] takes, and Agent accepts
// every layer. [durable.WithSessionID] names the session to create or
// resume. [durable.WithMiddleware] wraps the runs of that session. A
// harness option overlays the baseline for this build alone.
//
// Scalars override and tools append. A resolver is an additional source
// above the baseline sources. It wins the names it claims and leaves
// the rest standing. A caller that passes the resolvers and the working
// directory of a project here makes one harness serve many
// repositories.
//
// On its first run, a session with no history gets the entries from the
// configured [prompt.Seeder].
func (h *Harness) Agent(ctx context.Context, opts ...agent.Option) (*durable.Agent, error) {
	b, err := h.overlay(opts)
	if err != nil {
		return nil, err
	}

	res, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}

	tools := b.compileTools(res)
	env := b.env(b.model, tools, res)

	sys, err := b.system(ctx, env)
	if err != nil {
		return nil, err
	}

	// Agent builds the seed before the session exists, because
	// durable.New must register the middleware that injects it. The
	// middleware still decides per run whether the seed is needed,
	// against the leaf pointer. A resumed session therefore never uses
	// what is built here. The cost is one seeder call whose result the
	// build can discard. The gain is that a seeder error stops the
	// build, where the caller can act on it. It does not stop the
	// stream in the middle of a run.
	seed, err := b.seed(ctx, env)
	if err != nil {
		return nil, err
	}
	inject := &seedInjector{entries: seed}

	a, err := durable.New(
		ctx,
		b.factory,
		h.agentOpts(sys, tools, opts, durable.WithMiddleware(inject.middleware))...,
	)
	if err != nil {
		return nil, err
	}
	inject.leafID = a.LeafID

	return a, nil
}

// Env compiles a build and returns the [prompt.Env] that the build
// hands to its builders. The Env carries the resolved agent
// definitions, skills, and instructions, the tool list with the
// synthesized tools included, the model, and the working directory. Env
// creates no session and runs no seeder.
//
// Env is the read-only view of a build. A UI that lists the skills of a
// session gets its answer without an agent. A developer who asks what a
// given overlay resolves to gets the same answer. Options overlay the
// baseline exactly as they do in [Harness.Agent].
func (h *Harness) Env(ctx context.Context, opts ...agent.Option) (*prompt.Env, error) {
	b, err := h.overlay(opts)
	if err != nil {
		return nil, err
	}

	res, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}

	tools := b.compileTools(res)
	return b.env(b.model, tools, res), nil
}

// agentOpts assembles the option list for the durable agent: the
// harness options, then the caller options, then the compiled prompt
// and tools. The compiled values go last, so the compilation of the
// harness wins over anything the caller set directly.
//
// The seed option goes last of all. durable.New therefore registers the
// seed middleware after every middleware the caller passed, and the
// seed middleware ends up innermost. Every other middleware sees the
// run before the seed entries arrive.
func (h *Harness) agentOpts(
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

// env assembles the snapshot for the builders.
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

// system runs the configured [prompt.Builder]. compile always sets a
// builder. A build reaches the nil branch, and its agent gets no system
// prompt, only when someone assembled the build by hand.
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

// seed runs the configured [prompt.Seeder] and normalizes the entries
// it returns. It marks every message entry that is not ephemeral as
// meta. Seeded context is therefore visible to the model, hidden from
// the transcript, and durable, even when the seeder did not say so.
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

// seedInjector prepends the seed entries of a build to a run while the
// session has no persisted history.
//
// The signal is the leaf pointer, not a fired-once flag. A durable run
// persists its input before anything else. A run that dies before it
// persists leaves the leaf empty, and the next run seeds again. After
// any run persists, the seed is in history and never repeats. This is
// also why a resumed session never injects.
//
// leafID is wired after [durable.New] returns, because the middleware
// must be registered before the agent it asks about exists. This is
// safe. The body of the middleware runs only from [durable.Agent.Run],
// which no caller can reach until New hands the agent back.
//
// Known edge: a fork re-instantiates this middleware, but the closure
// still reads the leaf of the parent. If the parent runs first, a child
// forked from a session that never ran does not inject. A fork of a
// session with no history is already a degenerate case, and every other
// order is correct. A full correction needs the middleware to know
// which agent it wraps.
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
