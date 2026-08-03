package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/fs"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
	"github.com/sonnes/pi-go/pkg/session"
)

// --- static validation at New ---

func TestNewValidation(t *testing.T) {
	p := &mockProvider{}

	tests := []struct {
		name string
		opts []agent.Option
		err  string
	}{
		{
			name: "no catalog",
			opts: []agent.Option{WithDefaultModel("mock/small")},
			err:  "catalog is required",
		},
		{
			name: "no default model",
			opts: []agent.Option{WithCatalog(testCatalog(p))},
			err:  "default model is required",
		},
		{
			name: "unknown default model",
			opts: []agent.Option{
				WithCatalog(testCatalog(p)),
				WithDefaultModel("mock/nope"),
			},
			err: "unknown model",
		},
		{
			name: "reserved tool name",
			opts: []agent.Option{
				WithCatalog(testCatalog(p)),
				WithDefaultModel("mock/small"),
				WithTools(noopTool("skill")),
			},
			err: `tool name "skill" is reserved`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New[any](tt.opts...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.err)
		})
	}
}

func TestCallerSystemPromptIsRejected(t *testing.T) {
	p := &mockProvider{}
	ctx := context.Background()

	// The harness owns the system prompt: a caller's WithSystemPrompt
	// would be silently clobbered by the compiled one, so it is an
	// error at both option sites.
	_, err := New[any](
		WithCatalog(testCatalog(p)),
		WithDefaultModel("mock/small"),
		agent.WithSystemPrompt("I own the prompt"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use WithPromptBuilder")

	h := newTestHarness(t, p)
	_, err = h.Agent(ctx, agent.WithSystemPrompt("mine now"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use WithPromptBuilder")
}

func TestNewSucceedsWithMinimalConfig(t *testing.T) {
	h := newTestHarness(t, &mockProvider{})
	assert.Equal(t, "small", h.baseline.lm.Model().ID)
}

func TestNewFallsBackToPromptDefaults(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	ctx := context.Background()

	h := newTestHarness(t, p, WithWorkDir("/work"))
	a, err := h.Agent(ctx)
	require.NoError(t, err)

	_, err = a.Run(ctx, durable.Text("hi")).Wait()
	require.NoError(t, err)

	got := p.prompt(0)
	assert.Contains(t, got.System, "You are an agent.", "prompt.Default builds the system prompt")
	assert.Contains(t, got.Messages[0].Text(), "Working directory: /work", "prompt.DefaultSeed seeds the session")
}

// --- overlay: scalars override ---

func TestOverlayScalarsOverride(t *testing.T) {
	var env *prompt.Env
	p := &mockProvider{}

	h := newTestHarness(t, p, WithWorkDir("/baseline"), WithPromptBuilder(capturingBuilder(&env)))
	ctx := context.Background()

	_, err := h.Agent(ctx)
	require.NoError(t, err)
	assert.Equal(t, "/baseline", env.WorkDir)

	_, err = h.Agent(ctx, WithWorkDir("/per-build"))
	require.NoError(t, err)
	assert.Equal(t, "/per-build", env.WorkDir)

	// The baseline is not mutated by a build that overrode it.
	_, err = h.Agent(ctx)
	require.NoError(t, err)
	assert.Equal(t, "/baseline", env.WorkDir)
}

func TestOverlayDefaultModelOverride(t *testing.T) {
	var env *prompt.Env
	h := newTestHarness(t, &mockProvider{}, WithPromptBuilder(capturingBuilder(&env)))
	ctx := context.Background()

	_, err := h.Agent(ctx, WithDefaultModel("mock/large"))
	require.NoError(t, err)
	assert.Equal(t, "large", env.Model.ID)

	_, err = h.Agent(ctx, WithDefaultModel("mock/nope"))
	require.Error(t, err, "a per-build model is validated like the baseline one")
	assert.Contains(t, err.Error(), "unknown model")
}

func TestOverlayBuilderOverride(t *testing.T) {
	p := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	ctx := context.Background()

	h := newTestHarness(t, p)
	a, err := h.Agent(ctx, WithPromptBuilder(func(context.Context, *prompt.Env) (string, error) {
		return "PER-BUILD", nil
	}))
	require.NoError(t, err)

	_, err = a.Run(ctx, durable.Text("hi")).Wait()
	require.NoError(t, err)
	assert.Equal(t, "PER-BUILD", p.prompt(0).System)
}

// --- overlay: collections append ---

func TestOverlayResolverAppendsToBaseline(t *testing.T) {
	var env *prompt.Env

	baseline := def.Agents(
		def.Agent{Name: "reviewer", Source: "baseline"},
		def.Agent{Name: "writer", Source: "baseline"},
	)
	perBuild := def.Agents(def.Agent{Name: "reviewer", Source: "per-build"})

	h := newTestHarness(
		t,
		&mockProvider{},
		WithAgents(baseline),
		WithPromptBuilder(capturingBuilder(&env)),
	)
	ctx := context.Background()

	_, err := h.Agent(ctx, WithAgents(perBuild))
	require.NoError(t, err)

	// The build's resolver is an additional source, not a replacement: it
	// wins the name it claims and leaves the rest of the baseline standing.
	require.Len(t, env.Agents, 2)
	assert.Equal(t, "per-build", env.Agents[0].Source)
	assert.Equal(t, "writer", env.Agents[1].Name)
	assert.Equal(t, "baseline", env.Agents[1].Source)

	// Baseline-only builds are unaffected.
	_, err = h.Agent(ctx)
	require.NoError(t, err)
	require.Len(t, env.Agents, 2)
	assert.Equal(t, "baseline", env.Agents[0].Source)
}

func TestOverlaySkillsAppendToBaseline(t *testing.T) {
	var env *prompt.Env

	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(def.Skills(
			def.Skill{Name: "commit", Body: "baseline"},
			def.Skill{Name: "release", Body: "baseline"},
		)),
		WithPromptBuilder(capturingBuilder(&env)),
	)

	_, err := h.Agent(
		context.Background(),
		WithSkills(def.Skills(def.Skill{Name: "commit", Body: "per-build"})),
	)
	require.NoError(t, err)

	require.Len(t, env.Skills, 2)
	assert.Equal(t, "per-build", env.Skills[0].Body)
	assert.Equal(t, "release", env.Skills[1].Name)
}

func TestOverlayInstructionsConcatenateBaselineFirst(t *testing.T) {
	var env *prompt.Env

	h := newTestHarness(
		t,
		&mockProvider{},
		WithInstructions(def.Docs(def.Instructions{Source: "baseline", Content: "base rules"})),
		WithPromptBuilder(capturingBuilder(&env)),
	)

	_, err := h.Agent(
		context.Background(),
		WithInstructions(def.Docs(def.Instructions{Source: "per-build", Content: "build rules"})),
	)
	require.NoError(t, err)

	// Documents have no name to collide on, so both apply, the baseline's
	// first.
	require.Len(t, env.Instructions, 2)
	assert.Equal(t, "baseline", env.Instructions[0].Source)
	assert.Equal(t, "per-build", env.Instructions[1].Source)
}

func TestOverlayResolverWithNoBaselineIsUsedAlone(t *testing.T) {
	var env *prompt.Env

	h := newTestHarness(t, &mockProvider{}, WithPromptBuilder(capturingBuilder(&env)))

	_, err := h.Agent(
		context.Background(),
		WithAgents(def.Agents(def.Agent{Name: "reviewer", Source: "per-build"})),
	)
	require.NoError(t, err)
	require.Len(t, env.Agents, 1)
	assert.Equal(t, "per-build", env.Agents[0].Source)
}

func TestOverlayToolsAppend(t *testing.T) {
	var env *prompt.Env

	h := newTestHarness(
		t,
		&mockProvider{},
		WithTools(noopTool("read")),
		WithPromptBuilder(capturingBuilder(&env)),
	)
	ctx := context.Background()

	_, err := h.Agent(ctx, WithTools(noopTool("deploy")))
	require.NoError(t, err)
	assert.Equal(t, []string{"read", "deploy"}, toolNames(env.Tools), "baseline tools first, per-build appended")

	// Another agent from the same harness never sees the added tool.
	_, err = h.Agent(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"read"}, toolNames(env.Tools))
}

func TestOverlayToolWithBaselineNameOverridesInPlace(t *testing.T) {
	var env *prompt.Env
	h := newTestHarness(
		t,
		&mockProvider{},
		WithTools(noopTool("read"), noopTool("write")),
		WithPromptBuilder(capturingBuilder(&env)),
	)
	ctx := context.Background()

	// A per-build tool with a baseline name replaces it where it stood —
	// a session's sandboxed "read" over the process's — mirroring how a
	// per-build artifact overrides a baseline one.
	_, err := h.Agent(ctx, WithTools(descTool("read", "sandboxed")))
	require.NoError(t, err)
	assert.Equal(t, []string{"read", "write"}, toolNames(env.Tools), "the name keeps its position")
	assert.Equal(t, "sandboxed", env.Tools[0].Description, "the later registration wins")

	// Reserved names still error.
	_, err = h.Agent(ctx, WithTools(noopTool("skill")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool name "skill" is reserved`)
}

func TestDuplicateToolRegistrationIsLastWins(t *testing.T) {
	var env *prompt.Env
	h := newTestHarness(
		t,
		&mockProvider{},
		WithTools(
			noopTool("read"),
			noopTool("write"),
			descTool("read", "override"),
		),
		WithPromptBuilder(capturingBuilder(&env)),
	)

	_, err := h.Agent(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"read", "write"}, toolNames(env.Tools))
	assert.Equal(t, "override", env.Tools[0].Description)
}

// --- Env ---

func TestEnvExposesABuildWithoutASession(t *testing.T) {
	seeded := 0
	h := newTestHarness(
		t,
		&mockProvider{},
		WithWorkDir("/baseline"),
		WithTools(noopTool("read")),
		WithAgents(def.Agents(def.Agent{Name: "reviewer", Description: "reviews"})),
		WithSkills(def.Skills(def.Skill{Name: "commit", Description: "commits"})),
		WithSeed(func(context.Context, *prompt.Env) ([]session.Entry, error) {
			seeded++
			return nil, nil
		}),
	)
	ctx := context.Background()

	env, err := h.Env(ctx)
	require.NoError(t, err)
	assert.Equal(t, "/baseline", env.WorkDir)
	assert.Equal(t, "small", env.Model.ID)
	assert.Equal(t, []string{"reviewer"}, agentNames(env.Agents), "resolved agent defs are exposed")
	assert.Equal(t, []string{"commit"}, skillNames(env.Skills))
	assert.Equal(t, []string{"read", "skill"}, toolNames(env.Tools), "synthesized tools included")
	assert.Zero(t, seeded, "Env neither seeds nor touches a session")

	// Overlays apply exactly as they would to an agent build.
	over, err := h.Env(ctx, WithWorkDir("/per-build"))
	require.NoError(t, err)
	assert.Equal(t, "/per-build", over.WorkDir)
}

func TestBuildSurvivesMalformedArtifactFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		".agents/agents/reviewer.md":     "---\nname: reviewer\n---\nYou review.\n",
		".agents/agents/broken.md":       "---\nname: [unterminated\n---\nbody\n",
		".agents/skills/commit/SKILL.md": "---\nname: commit\n---\nBody.\n",
		".agents/skills/broken/SKILL.md": "---\nname: [unterminated\n---\nBody.\n",
	}
	for name, content := range files {
		full := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}

	proj := os.DirFS(dir)
	h := newTestHarness(
		t,
		&mockProvider{},
		WithAgents(fs.Agents(proj, ".agents/agents")),
		WithSkills(fs.Skills(proj, ".agents/skills")),
	)

	// One malformed file must never take a session down with it: the
	// build succeeds and exposes the artifacts that parsed.
	env, err := h.Env(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"reviewer"}, agentNames(env.Agents))
	assert.Equal(t, []string{"commit"}, skillNames(env.Skills))
}

func TestOverlayLeavesHarnessUsableConcurrently(t *testing.T) {
	var env *prompt.Env
	h := newTestHarness(
		t,
		&mockProvider{},
		WithTools(noopTool("read")),
		WithPromptBuilder(capturingBuilder(&env)),
	)
	ctx := context.Background()

	_, err := h.Agent(ctx, WithTools(noopTool("a")))
	require.NoError(t, err)
	_, err = h.Agent(ctx, WithTools(noopTool("b")))
	require.NoError(t, err)

	assert.Equal(t, []string{"read", "b"}, toolNames(env.Tools), "one build's overlay never leaks into another")
}
