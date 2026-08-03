package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/session"
)

func TestOptionsMixLayers(t *testing.T) {
	p := &mockProvider{}
	store := session.NewMemoryStore[any]()

	opts := []agent.Option{
		WithCatalog(testCatalog(p)),
		WithDefaultModel("mock/small"),
		WithTools(noopTool("read")),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
		agent.WithMaxTurns(3),
	}

	cfg := agent.ApplyOptions(opts...)

	// Each layer reads its own extension slot; agent options land on the
	// config itself. One flat list, three consumers.
	assert.Equal(t, 3, cfg.MaxTurns)
	assert.NotNil(t, cfg.Extensions["harness"])
	assert.NotNil(t, cfg.Extensions["durable"])

	e := slot.From(cfg)
	assert.Equal(t, "mock/small", e.defaultModel)
	require.Len(t, e.tools, 1)
	assert.Equal(t, "read", e.tools[0].Info().Name)
}

func TestRepeatedResolverOptionsAppend(t *testing.T) {
	a := &fakeResolver{name: "a"}
	b := &fakeResolver{name: "b"}
	c := &fakeResolver{name: "c"}

	// Resolvers are a list of sources, lowest first, so repeated options
	// add to it. Whether one displaces another is settled at resolve time,
	// by name.
	cfg := agent.ApplyOptions(
		WithSkills(a),
		WithSkills(b, c),
	)
	e := slot.From(cfg)
	require.Len(t, e.skills, 3)
	assert.Same(t, a, e.skills[0])
	assert.Same(t, b, e.skills[1])
	assert.Same(t, c, e.skills[2])
}

func TestMergeLeavesBothInputsUntouched(t *testing.T) {
	base := &ext{
		workDir: "/baseline",
		skills:  []def.SkillResolver{&fakeResolver{name: "base"}},
		tools:   []ai.Tool{noopTool("read")},
	}
	over := &ext{
		workDir: "/per-build",
		skills:  []def.SkillResolver{&fakeResolver{name: "over"}},
		tools:   []ai.Tool{noopTool("deploy")},
	}

	got := base.merge(over)

	assert.Equal(t, "/per-build", got.workDir, "scalars override")
	require.Len(t, got.skills, 2, "sources append, baseline first")
	require.Len(t, got.tools, 2)

	// Neither input is disturbed, which is what lets one harness serve
	// many concurrent builds.
	assert.Equal(t, "/baseline", base.workDir)
	require.Len(t, base.skills, 1)
	require.Len(t, over.skills, 1)
}

func TestIsZeroSpotsAnEmptyOverlay(t *testing.T) {
	assert.True(t, (&ext{}).isZero())
	assert.False(t, (&ext{workDir: "/x"}).isZero())
	assert.False(t, (&ext{skills: []def.SkillResolver{&fakeResolver{}}}).isZero())
	assert.False(t, (&ext{instructions: []def.InstructionResolver{&fakeResolver{}}}).isZero())
}
