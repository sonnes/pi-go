package harness

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/harness/def"
)

// cliCatalog registers a mock provider under "mock" and an agent
// factory under "cli". A "cli/..." spec therefore has no language model
// and no entry in the model index, exactly like a subprocess CLI.
func cliCatalog(p *mockProvider, f agent.Factory) *catalog.Catalog {
	c := testCatalog(p)
	c.RegisterAgent("cli", f)
	return c
}

// TestAgentKindCompilesAndRuns is the whole point of the factory seam: a
// harness over a spec the catalog serves with an agent factory, not a
// language model, builds and runs.
func TestAgentKindCompilesAndRuns(t *testing.T) {
	ctx := context.Background()
	inner := &mockProvider{responses: []*ai.EventStream{textStream("from the cli")}}

	var (
		mu    sync.Mutex
		specs []string
		sys   []string
	)
	factory := func(spec string, opts ...agent.Option) (agent.Agent, error) {
		cfg := agent.ApplyOptions(opts...)
		mu.Lock()
		specs = append(specs, spec)
		sys = append(sys, cfg.SystemPrompt)
		mu.Unlock()
		return agent.New(ai.NewLanguageModel(ai.Model{ID: "opus"}, inner), opts...), nil
	}

	h, err := New(
		WithCatalog(cliCatalog(&mockProvider{}, factory)),
		WithDefaultModel("cli/opus"),
		WithInstructions(def.Docs(def.Instructions{
			Source:  "AGENTS.md",
			Content: "House rule: be terse.",
		})),
	)
	require.NoError(t, err)

	a, err := h.Agent(ctx, durable.WithSessionID("s1"))
	require.NoError(t, err)
	defer a.Close()

	msgs, err := a.Run(ctx, durable.Text("hi")).Wait()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "from the cli", msgs[0].Text())

	// The factory saw the full spec and the prompt the harness compiled.
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, specs, 1)
	assert.Equal(t, "cli/opus", specs[0])
	assert.Contains(t, sys[0], "House rule: be terse.")
}

// TestAgentKindEnvSynthesizesModel checks that a build over an agent
// kind still gives the prompt builders a model. The catalog serves no
// metadata for such a spec, so the ID stands in for it.
func TestAgentKindEnvSynthesizesModel(t *testing.T) {
	factory := func(string, ...agent.Option) (agent.Agent, error) { return nil, nil }

	h, err := New(
		WithCatalog(cliCatalog(&mockProvider{}, factory)),
		WithDefaultModel("cli/opus"),
	)
	require.NoError(t, err)

	env, err := h.Env(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "opus", env.Model.ID)
}

// TestLanguageModelKindKeepsItsMetadata checks that the ordinary path is
// unchanged: a registered model still reaches the builders whole.
func TestLanguageModelKindKeepsItsMetadata(t *testing.T) {
	h := newTestHarness(t, &mockProvider{})

	env, err := h.Env(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "small", env.Model.ID)
	assert.Equal(t, "Mock Small", env.Model.Name)
}

// TestUnknownDefaultModelStillFailsEarly checks that moving off
// LanguageModel did not cost the eager validation. A default model
// nobody serves must stop New, not the first run.
func TestUnknownDefaultModelStillFailsEarly(t *testing.T) {
	_, err := New(
		WithCatalog(testCatalog(&mockProvider{})),
		WithDefaultModel("mock/nope"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default model")
}

// TestAgentKindFactoryErrorReachesRun checks that a factory that cannot
// build reports on the run, not at build time. Whether the CLI is
// present is a fact about the moment of the run.
func TestAgentKindFactoryErrorReachesRun(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("cli not on PATH")

	h, err := New(
		WithCatalog(cliCatalog(&mockProvider{}, func(string, ...agent.Option) (agent.Agent, error) {
			return nil, sentinel
		})),
		WithDefaultModel("cli/opus"),
	)
	require.NoError(t, err)

	a, err := h.Agent(ctx, durable.WithSessionID("s1"))
	require.NoError(t, err)
	defer a.Close()

	_, err = a.Run(ctx, durable.Text("hi")).Wait()
	assert.ErrorIs(t, err, sentinel)
}
