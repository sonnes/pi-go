package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/fs"
)

// --- synthesis ---

func TestSynthesizedToolsAppearOnlyWhenArtifactsExist(t *testing.T) {
	ctx := context.Background()

	bare := newTestHarness(t, &mockProvider{})
	res, err := bare.baseline.resolve(ctx)
	require.NoError(t, err)
	tools := bare.baseline.compileTools(res)
	assert.Nil(t, findTool(tools, skillToolName), "no skills, no skill tool")

	full := newTestHarness(
		t,
		&mockProvider{},
		WithTools(noopTool("read")),
		WithSkills(def.Skills(def.Skill{Name: "commit", Description: "commits"})),
		WithAgents(def.Agents(def.Agent{Name: "reviewer", Description: "reviews"})),
	)
	res, err = full.baseline.resolve(ctx)
	require.NoError(t, err)
	tools = full.baseline.compileTools(res)
	assert.NotNil(t, findTool(tools, skillToolName))

	// Agent definitions are resolved data, not behavior. The harness
	// synthesizes no tool from them. Nothing beyond the registered tools
	// and the skill tool reaches the model.
	assert.Nil(t, findTool(tools, agentToolName), "resolved agent defs synthesize no tool")
	require.Len(t, tools, 2)
	require.Len(t, res.agents, 1, "the definitions are still resolved and exposed")
}

func TestSkillToolSchemaListsNames(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(def.Skills(
			def.Skill{Name: "commit", Description: "writes a commit"},
			def.Skill{Name: "review", Description: "reviews a diff"},
		)),
	)
	res, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)

	tool := findTool(h.baseline.compileTools(res), skillToolName)
	require.NotNil(t, tool)

	schema := tool.Info().InputSchema
	require.NotNil(t, schema)
	nameProp := schema.Properties["name"]
	require.NotNil(t, nameProp)
	assert.Equal(t, []any{"commit", "review"}, nameProp.Enum)

	// The description carries the catalogue. A model that chooses a
	// skill never has to guess what the skill does.
	assert.Contains(t, tool.Info().Description, "writes a commit")

	// A skill load is a pure read, so a batch that contains one does not
	// serialize.
	assert.True(t, tool.Info().Parallel)
}

func TestScopedArtifactsAdvertiseTheirDirectory(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(def.Skills(
			def.Skill{Name: "deploy", Description: "Ships it."},
			def.Skill{Name: "deploy", Scope: "apps/web", Description: "Ships it."},
		)),
	)
	res, err := h.baseline.resolve(context.Background())
	require.NoError(t, err)

	tool := findTool(h.baseline.compileTools(res), skillToolName)
	require.NotNil(t, tool)
	assert.Equal(
		t,
		[]any{"deploy", "apps/web:deploy"},
		tool.Info().InputSchema.Properties["name"].Enum,
	)

	// Two skills that do the same work in different places differ by
	// where they apply, not by their descriptions.
	assert.Contains(t, tool.Info().Description, "Ships it. Applies to work in apps/web.")
}

// --- skill tool behavior ---

func TestSkillToolServesTheBody(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(def.Skills(def.Skill{
			Name:        "commit",
			Description: "writes a commit",
			Dir:         "/skills/commit",
			Body:        "SKILL BODY",
		})),
	)
	ctx := context.Background()
	res, err := h.baseline.resolve(ctx)
	require.NoError(t, err)

	tool := findTool(h.baseline.compileTools(res), skillToolName)
	require.NotNil(t, tool)

	// The body stays out of the system prompt and arrives through the
	// tool. The directory of the skill prefixes the body when the skill
	// has one.
	out, err := tool.Run(ctx, ai.ToolCallReq{ID: "c1", Input: `{"name":"commit"}`})
	require.NoError(t, err)
	assert.False(t, out.IsError)
	assert.Contains(t, out.Content, "SKILL BODY")
	assert.Contains(t, out.Content, "/skills/commit")
}

func TestSkillBodyIsPartOfTheBuildSnapshot(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "skills/commit/SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o755))
	write := func(body string) {
		content := "---\nname: commit\n---\n" + body + "\n"
		require.NoError(t, os.WriteFile(skillPath, []byte(content), 0o644))
	}
	write("old body")

	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(fs.Skills(os.DirFS(dir), "skills")),
	)
	ctx := context.Background()

	res, err := h.baseline.resolve(ctx)
	require.NoError(t, err)
	tool := findTool(h.baseline.compileTools(res), skillToolName)
	require.NotNil(t, tool)

	// The file changes after the build captured its snapshot.
	write("new body")

	// The artifacts of a built agent are a snapshot for its lifetime,
	// the body included. The re-read at call time used to be the one
	// escape from that rule.
	out, err := tool.Run(ctx, ai.ToolCallReq{ID: "c1", Input: `{"name":"commit"}`})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "old body")
	assert.NotContains(t, out.Content, "new body")

	// The next build reads the file again and sees the edit.
	res, err = h.baseline.resolve(ctx)
	require.NoError(t, err)
	tool = findTool(h.baseline.compileTools(res), skillToolName)
	out, err = tool.Run(ctx, ai.ToolCallReq{ID: "c2", Input: `{"name":"commit"}`})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "new body")
}

func TestSkillToolUnknownSkillIsToolError(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithSkills(def.Skills(def.Skill{Name: "commit", Body: "b"})),
	)
	ctx := context.Background()
	res, err := h.baseline.resolve(ctx)
	require.NoError(t, err)

	tool := findTool(h.baseline.compileTools(res), skillToolName)
	out, err := tool.Run(ctx, ai.ToolCallReq{ID: "c1", Input: `{"name":"nope"}`})
	require.NoError(t, err, "a bad argument is a tool error, not a run failure")
	assert.True(t, out.IsError)
	assert.Contains(t, out.Content, `unknown skill "nope"`)
}
