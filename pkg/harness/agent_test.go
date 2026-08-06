package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
	"github.com/sonnes/pi-go/pkg/session"
)

// --- the build ---

func TestAgentBuildsEnv(t *testing.T) {
	var env *prompt.Env
	p := &mockProvider{}

	h := newTestHarness(
		t,
		p,
		WithWorkDir("/work"),
		WithTools(noopTool("read")),
		WithAgents(def.Agents(def.Agent{Name: "reviewer", Description: "reviews"})),
		WithSkills(def.Skills(def.Skill{Name: "commit", Description: "commits"})),
		WithInstructions(def.Docs(def.Instructions{Source: "AGENTS.md", Content: "rules"})),
		WithPromptBuilder(capturingBuilder(&env)),
	)

	_, err := h.Agent(context.Background())
	require.NoError(t, err)

	require.NotNil(t, env)
	assert.Equal(t, "small", env.Model.ID)
	assert.Equal(t, "/work", env.WorkDir)
	require.Len(t, env.Agents, 1)
	assert.Equal(t, "reviewer", env.Agents[0].Name)
	require.Len(t, env.Skills, 1)
	assert.Equal(t, "commit", env.Skills[0].Name)
	require.Len(t, env.Instructions, 1)
	assert.Equal(t, "rules", env.Instructions[0].Content)
	assert.Contains(t, toolNames(env.Tools), "read")
}

func TestAgentUsesBuiltSystemPrompt(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("hi")}}
	var env *prompt.Env

	h := newTestHarness(t, p, WithPromptBuilder(capturingBuilder(&env)))

	a, err := h.Agent(context.Background())
	require.NoError(t, err)

	_, err = a.Run(context.Background(), durable.Text("hello")).Wait()
	require.NoError(t, err)

	require.Equal(t, 1, p.promptCount())
	assert.Equal(t, "SYSTEM", p.prompt(0).System)
}

func TestAgentResolvesFreshPerBuild(t *testing.T) {
	var env *prompt.Env
	list := &mutableResolver{}

	h := newTestHarness(
		t,
		&mockProvider{},
		WithAgents(list),
		WithPromptBuilder(capturingBuilder(&env)),
	)
	ctx := context.Background()

	_, err := h.Agent(ctx)
	require.NoError(t, err)
	assert.Empty(t, env.Agents)

	list.set(def.Agent{Name: "reviewer", Description: "added later"})

	_, err = h.Agent(ctx)
	require.NoError(t, err)
	require.Len(t, env.Agents, 1, "the next build sees the new definition")
	assert.Equal(t, "reviewer", env.Agents[0].Name)
}

func TestAgentPropagatesResolverError(t *testing.T) {
	boom := errors.New("disk on fire")
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(&fakeResolver{err: boom}),
	)

	_, err := h.Agent(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

func TestAgentForwardsCallerOptions(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	ctx := context.Background()

	h := newTestHarness(t, p)
	a, err := h.Agent(ctx, durable.WithSessionID("caller-chosen"), agent.WithMaxTurns(1))
	require.NoError(t, err)

	assert.Equal(t, "caller-chosen", a.SessionID())
}

// --- seeding ---

func TestSeedInjectedOnFirstRunOnly(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{
		textStream("one"),
		textStream("two"),
	}}
	store := session.NewMemoryStore()
	ctx := context.Background()

	h := newTestHarness(
		t,
		p,
		WithSeed(seedText("ENVIRONMENT")),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)

	a, err := h.Agent(ctx)
	require.NoError(t, err)

	_, err = a.Run(ctx, durable.Text("first")).Wait()
	require.NoError(t, err)

	// The seed reached the model ahead of the user's input.
	first := p.prompt(0)
	require.GreaterOrEqual(t, len(first.Messages), 2)
	assert.Equal(t, "ENVIRONMENT", first.Messages[0].Text())
	assert.Equal(t, "first", first.Messages[1].Text())

	// It is durable, and hidden from the transcript.
	entries, err := a.Entries(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, countText(entries, "ENVIRONMENT"))

	transcript, err := a.Transcript(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, countText(transcript, "ENVIRONMENT"), "meta entries stay out of the transcript")

	// A second run does not seed again.
	_, err = a.Run(ctx, durable.Text("second")).Wait()
	require.NoError(t, err)

	entries, err = a.Entries(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, countText(entries, "ENVIRONMENT"))
}

func TestSeedSurvivesAFailedFirstRun(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	store := &flakyStore{Store: session.NewMemoryStore(), fail: true}
	ctx := context.Background()

	h := newTestHarness(
		t,
		p,
		WithSeed(seedText("ENVIRONMENT")),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)

	a, err := h.Agent(ctx)
	require.NoError(t, err)

	// The first run dies persisting its input, before the model call —
	// nothing durable happened, including the seed.
	_, err = a.Run(ctx, durable.Text("first")).Wait()
	require.Error(t, err)

	// The store recovers. The retry is still the session's first durable
	// run, so it must carry the seed again.
	store.fail = false
	_, err = a.Run(ctx, durable.Text("retry")).Wait()
	require.NoError(t, err)

	first := p.prompt(0)
	require.NotEmpty(t, first.Messages)
	assert.Equal(t, "ENVIRONMENT", first.Messages[0].Text(), "the seed leads the retried first run")

	entries, err := a.Entries(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, countText(entries, "ENVIRONMENT"))
}

func TestResumedSessionDoesNotReseed(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{
		textStream("one"),
		textStream("two"),
	}}
	store := session.NewMemoryStore()
	ctx := context.Background()

	h := newTestHarness(
		t,
		p,
		WithSeed(seedText("ENVIRONMENT")),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)

	first, err := h.Agent(ctx)
	require.NoError(t, err)
	_, err = first.Run(ctx, durable.Text("first")).Wait()
	require.NoError(t, err)

	// A second instance resumes the same session: history already
	// carries the seed, so there is nothing to inject.
	resumed, err := h.Agent(ctx)
	require.NoError(t, err)
	_, err = resumed.Run(ctx, durable.Text("second")).Wait()
	require.NoError(t, err)

	entries, err := resumed.Entries(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, countText(entries, "ENVIRONMENT"))
}

func TestNoSeedDisablesSeeding(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	ctx := context.Background()

	// WithSeed(nil) would mean "use the default"; NoSeed is how a caller
	// says "seed nothing".
	h := newTestHarness(t, p, WithSeed(prompt.NoSeed))

	a, err := h.Agent(ctx)
	require.NoError(t, err)
	_, err = a.Run(ctx, durable.Text("hi")).Wait()
	require.NoError(t, err)

	first := p.prompt(0)
	require.Len(t, first.Messages, 1, "no environment block precedes the input")
	assert.Equal(t, "hi", first.Messages[0].Text())
}

func TestSeedEntriesAreMeta(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	ctx := context.Background()

	h := newTestHarness(t, p, WithSeed(func(context.Context, *prompt.Env) ([]session.Entry, error) {
		// A seeder that forgets to mark its entry still gets meta
		// semantics — the harness owns that contract.
		return []session.Entry{durable.Text("ENVIRONMENT")}, nil
	}))

	a, err := h.Agent(ctx)
	require.NoError(t, err)
	_, err = a.Run(ctx, durable.Text("hi")).Wait()
	require.NoError(t, err)

	entries, err := a.Entries(ctx)
	require.NoError(t, err)
	for _, e := range entries {
		me, ok := session.AsMessageEntry(e)
		if ok && me.Text() == "ENVIRONMENT" {
			assert.True(t, me.Meta)
			return
		}
	}
	t.Fatal("seed entry not found")
}

// Seeding is implemented as durable middleware, registered last so it
// ends up innermost. These two pin that arrangement from the outside.

func TestSeedIsInnermostMiddleware(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	ctx := context.Background()

	// A middleware registered by the caller sees the run before the
	// harness's seeding adds anything to it.
	var sawAtEntry int
	spy := func(next durable.Runner) durable.Runner {
		return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
			sawAtEntry = len(entries)
			return next(ctx, entries...)
		}
	}

	h := newTestHarness(t, p, WithSeed(seedText("ENVIRONMENT")))
	a, err := h.Agent(ctx, durable.WithMiddleware(spy))
	require.NoError(t, err)

	_, err = a.Run(ctx, durable.Text("hi")).Wait()
	require.NoError(t, err)

	assert.Equal(t, 1, sawAtEntry, "the caller's middleware runs outside the seed")
	assert.Contains(t, promptText(p.prompt(0)), "ENVIRONMENT", "and the seed still lands")
}

func TestSeederRunsEvenWhenTheSessionIsResumed(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("one"), textStream("two")}}
	store := session.NewMemoryStore()
	ctx := context.Background()

	seeds := 0
	h := newTestHarness(
		t,
		p,
		WithSeed(func(context.Context, *prompt.Env) ([]session.Entry, error) {
			seeds++
			return []session.Entry{durable.Text("ENVIRONMENT")}, nil
		}),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)

	first, err := h.Agent(ctx)
	require.NoError(t, err)
	_, err = first.Run(ctx, durable.Text("first")).Wait()
	require.NoError(t, err)

	resumed, err := h.Agent(ctx)
	require.NoError(t, err)
	_, err = resumed.Run(ctx, durable.Text("second")).Wait()
	require.NoError(t, err)

	// The seed is built eagerly, before the session is bound, so the
	// seeder runs per build — the resumed build simply discards what it
	// produced. The cost of keeping seeder errors at build time.
	assert.Equal(t, 2, seeds, "the seeder runs on every build")

	entries, err := resumed.Entries(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, countText(entries, "ENVIRONMENT"), "but only a fresh session injects it")
}

// --- durable verbs ---

func TestDurableVerbsPassThrough(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	ctx := context.Background()

	h := newTestHarness(t, p, durable.WithSessionID("s1"))
	a, err := h.Agent(ctx)
	require.NoError(t, err)
	defer a.Close()

	assert.Equal(t, "s1", a.SessionID())
	assert.Empty(t, a.LeafID())

	_, err = a.Run(ctx, durable.Text("hi")).Wait()
	require.NoError(t, err)

	leaf := a.LeafID()
	assert.NotEmpty(t, leaf)

	forked, err := a.Fork(ctx, "s2")
	require.NoError(t, err)
	assert.Equal(t, "s2", forked.SessionID())

	require.NoError(t, a.Branch(ctx, leaf))
	assert.Equal(t, leaf, a.LeafID())

	assert.NotEmpty(t, a.Messages())
}
