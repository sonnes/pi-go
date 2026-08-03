package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/harness/def"
)

// --- the basics ---

func TestResolveWithNoResolversIsEmpty(t *testing.T) {
	h := newTestHarness(t, &mockProvider{})

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got.agents)
	assert.Empty(t, got.skills)
	assert.Empty(t, got.instructions)
}

func TestResolveCoversEveryKind(t *testing.T) {
	r := &fakeResolver{
		agents: []def.Agent{{Name: "reviewer", Source: "proj"}},
		skills: []def.Skill{{Name: "review", Body: "proj"}},
		docs:   []def.Instructions{{Source: "proj", Content: "a"}},
	}

	h := newTestHarness(t, &mockProvider{}, WithAgents(r), WithSkills(r), WithInstructions(r))

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)

	require.Len(t, got.agents, 1)
	assert.Equal(t, "proj", got.agents[0].Source)
	require.Len(t, got.skills, 1)
	assert.Equal(t, "proj", got.skills[0].Body)
	require.Len(t, got.instructions, 1)
	assert.Equal(t, "proj", got.instructions[0].Source)
}

func TestResolveRunsResolversEveryTime(t *testing.T) {
	r := &fakeResolver{skills: []def.Skill{{Name: "review"}}}
	h := newTestHarness(t, &mockProvider{}, WithSkills(r))
	ctx := context.Background()

	_, err := h.baseline.resolve(ctx)
	require.NoError(t, err)
	_, err = h.baseline.resolve(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, r.calls, "resolution is never cached across builds")
}

// --- qualification ---

func TestResolveQualifiesScopedNames(t *testing.T) {
	h := newTestHarness(t, &mockProvider{}, WithSkills(&fakeResolver{skills: []def.Skill{
		{Name: "deploy"},
		{Name: "deploy", Scope: "apps/web"},
		{Name: "deploy", Scope: "apps/api"},
	}}))

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)

	// An unscoped name stands alone; a scoped one is qualified with the
	// directory it governs, so all three coexist.
	assert.Equal(
		t,
		[]string{"deploy", "apps/web:deploy", "apps/api:deploy"},
		skillNames(got.skills),
	)
}

func TestResolveQualifiesScopedAgents(t *testing.T) {
	h := newTestHarness(t, &mockProvider{}, WithAgents(&fakeResolver{agents: []def.Agent{
		{Name: "reviewer"},
		{Name: "reviewer", Scope: "apps/web"},
	}}))

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"reviewer", "apps/web:reviewer"}, agentNames(got.agents))
}

// --- union across sources ---

func TestResolveUnionsAcrossResolvers(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithAgents(
			&fakeResolver{agents: []def.Agent{{Name: "reviewer", Source: "builtin"}}},
			&fakeResolver{agents: []def.Agent{{Name: "writer", Source: "home"}}},
		),
	)

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"reviewer", "writer"}, agentNames(got.agents))
}

func TestResolveScopeKeepsNamesApartAcrossResolvers(t *testing.T) {
	// The same base name from two sources, at different scopes, so
	// neither displaces the other.
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(
			&fakeResolver{skills: []def.Skill{{Name: "deploy", Body: "home"}}},
			&fakeResolver{skills: []def.Skill{{Name: "deploy", Scope: "apps/web", Body: "project"}}},
		),
	)

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"deploy", "apps/web:deploy"}, skillNames(got.skills))
	assert.Equal(t, "home", got.skills[0].Body)
	assert.Equal(t, "project", got.skills[1].Body)
}

// --- collisions ---

func TestResolveLastResolverWinsKeepingPosition(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithAgents(
			&fakeResolver{agents: []def.Agent{
				{Name: "reviewer", Source: "home"},
				{Name: "writer", Source: "home"},
			}},
			&fakeResolver{agents: []def.Agent{{Name: "reviewer", Source: "project"}}},
		),
	)

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)

	// Replacement is whole-definition, and the name keeps the position it
	// first appeared at, so an override does not reshuffle the listing.
	require.Len(t, got.agents, 2)
	assert.Equal(t, "reviewer", got.agents[0].Name)
	assert.Equal(t, "project", got.agents[0].Source)
	assert.Equal(t, "writer", got.agents[1].Name)
	assert.Equal(t, "home", got.agents[1].Source)
}

func TestResolveLastResolverWinsAtTheSameScope(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(
			&fakeResolver{skills: []def.Skill{{Name: "deploy", Scope: "apps/web", Body: "home"}}},
			&fakeResolver{skills: []def.Skill{{Name: "deploy", Scope: "apps/web", Body: "project"}}},
		),
	)

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)
	require.Len(t, got.skills, 1)
	assert.Equal(t, "apps/web:deploy", got.skills[0].Name)
	assert.Equal(t, "project", got.skills[0].Body)
}

func TestResolveDuplicateWithinResolverFails(t *testing.T) {
	tests := []struct {
		name string
		opt  agent.Option
		errs []string
	}{
		{
			name: "same agent name twice",
			opt: WithAgents(&fakeResolver{agents: []def.Agent{
				{Name: "reviewer", Source: "a.md"},
				{Name: "reviewer", Source: "b.md"},
			}}),
			errs: []string{`duplicate agent "reviewer"`, `qualified "reviewer"`},
		},
		{
			name: "same name twice",
			opt: WithSkills(&fakeResolver{skills: []def.Skill{
				{Name: "review", Source: "a"},
				{Name: "review", Source: "b"},
			}}),
			errs: []string{`duplicate skill "review"`, `qualified "review"`},
		},
		{
			// Overriding is between sources, never within one, so a
			// duplicate here is a malformed resolver rather than a layering
			// decision. The name a model sees is not the name on disk, so
			// the error prints the raw name, the scope, and the qualified
			// name.
			name: "same scope twice",
			opt: WithSkills(&fakeResolver{skills: []def.Skill{
				{Name: "deploy", Scope: "apps/web", Source: "a"},
				{Name: "deploy", Scope: "apps/web", Source: "b"},
			}}),
			errs: []string{
				`duplicate skill "deploy"`,
				`scope "apps/web"`,
				`qualified "apps/web:deploy"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHarness(t, &mockProvider{}, tt.opt)
			_, err := h.baseline.resolve(context.Background())
			require.Error(t, err)
			for _, want := range tt.errs {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestResolveRejectsColonInName(t *testing.T) {
	// ":" is how the harness spells qualification. A literal
	// "apps/web:deploy" from a resolver must not silently collide with a
	// scoped "deploy" from another.
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(&fakeResolver{skills: []def.Skill{
			{Name: "apps/web:deploy", Source: "s.md"},
		}}),
	)

	_, err := h.baseline.resolve(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"apps/web:deploy"`)
	assert.Contains(t, err.Error(), `":"`)
	assert.Contains(t, err.Error(), `"s.md"`, "the error names the source")
}

func TestResolveUnnamedArtifactFails(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(def.Skills(def.Skill{Description: "nameless"})),
	)
	_, err := h.baseline.resolve(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skill has no name")

	h = newTestHarness(
		t,
		&mockProvider{},
		WithAgents(def.Agents(def.Agent{Description: "nameless"})),
	)
	_, err = h.baseline.resolve(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent has no name")
}

func TestResolveDoesNotValidateAgentDefs(t *testing.T) {
	// Model and Tools are advisory metadata: the harness consumes
	// neither, so a definition asking for a model this catalog lacks or
	// tools this toolbox lacks still resolves. The product that acts on
	// the definition checks what it uses.
	h := newTestHarness(
		t,
		&mockProvider{},
		WithTools(noopTool("read")),
		WithAgents(def.Agents(def.Agent{
			Name:  "reviewer",
			Model: "mock/nope",
			Tools: []string{"no-such-tool"},
		})),
	)

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)
	require.Len(t, got.agents, 1)
	assert.Equal(t, "mock/nope", got.agents[0].Model)
	assert.Equal(t, []string{"no-such-tool"}, got.agents[0].Tools)
}

// --- instructions ---

func TestResolveInstructionsConcatenateAcrossResolvers(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithInstructions(
			&fakeResolver{docs: []def.Instructions{{Source: "home", Content: "home rules"}}},
			&fakeResolver{docs: []def.Instructions{{Source: "project", Content: "project rules"}}},
		),
	)

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)

	// Documents have no name to qualify or override, so every source
	// applies.
	require.Len(t, got.instructions, 2)
	assert.Equal(t, "home", got.instructions[0].Source)
	assert.Equal(t, "project", got.instructions[1].Source)
}

func TestResolveInstructionsSkipIdenticalContent(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithInstructions(
			&fakeResolver{docs: []def.Instructions{{Source: "home", Content: "Shared rules."}}},
			&fakeResolver{docs: []def.Instructions{{Source: "project", Content: "Shared rules."}}},
			&fakeResolver{docs: []def.Instructions{{Source: "project", Content: "Own rules."}}},
		),
	)

	got, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)

	// The same document registered twice — the double-registration
	// footgun — costs its tokens once. The first occurrence keeps its
	// position and provenance.
	require.Len(t, got.instructions, 2)
	assert.Equal(t, "home", got.instructions[0].Source)
	assert.Equal(t, "Own rules.", got.instructions[1].Content)
}

// --- provenance ---

func TestResolveErrorCarriesProvenance(t *testing.T) {
	boom := errors.New("disk on fire")
	h := newTestHarness(
		t,
		&mockProvider{},
		WithAgents(
			&fakeResolver{agents: []def.Agent{{Name: "reviewer"}}},
			&fakeResolver{err: boom},
		),
	)

	_, err := h.baseline.resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "agent resolver 1", "the failing source's position is named")
	assert.Contains(t, err.Error(), "fakeResolver", "and its type")
}
