package def_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/harness/def"
)

func TestLiteralAgents(t *testing.T) {
	r := def.Agents(
		def.Agent{Name: "reviewer", Description: "reviews code"},
		def.Agent{Name: "writer", Description: "writes code"},
	)

	got, err := r.Agents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "reviewer", got[0].Name)
	assert.Equal(t, "writer", got[1].Name)

	// The returned slice is a copy. A change cannot corrupt the resolver.
	got[0].Name = "mutated"
	again, err := r.Agents(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "reviewer", again[0].Name)
}

func TestLiteralSkills(t *testing.T) {
	r := def.Skills(
		def.Skill{Name: "review", Body: "a"},
		def.Skill{Name: "commit", Body: "b"},
	)

	got, err := r.Skills(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "review", got[0].Name)
	assert.Equal(t, "commit", got[1].Name)

	// The returned slice is a copy. A change cannot corrupt the resolver.
	got[0].Name = "mutated"
	again, err := r.Skills(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "review", again[0].Name)
}

func TestLiteralDocs(t *testing.T) {
	r := def.Docs(
		def.Instructions{Source: "AGENTS.md", Content: "root rules"},
		def.Instructions{Source: "web/AGENTS.md", Content: "web rules"},
	)

	got, err := r.Instructions(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "AGENTS.md", got[0].Source)
	assert.Equal(t, "web/AGENTS.md", got[1].Source)

	got[0].Content = "mutated"
	again, err := r.Instructions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "root rules", again[0].Content)
}

func TestLiteralResolversEmpty(t *testing.T) {
	ctx := context.Background()

	agents, err := def.Agents().Agents(ctx)
	require.NoError(t, err)
	assert.Empty(t, agents)

	skills, err := def.Skills().Skills(ctx)
	require.NoError(t, err)
	assert.Empty(t, skills)

	docs, err := def.Docs().Instructions(ctx)
	require.NoError(t, err)
	assert.Empty(t, docs)
}
