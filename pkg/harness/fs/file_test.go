package fs_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/harness/fs"
)

// --- agent definitions ---

func TestAgentFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		err     string
	}{
		{
			name: "full frontmatter",
			content: `---
name: reviewer
description: Reviews code for defects
model: mock/large
tools: [read, grep]
---
You review code.
`,
		},
		{
			name: "missing frontmatter",
			content: `You review code.
`,
			err: "missing frontmatter",
		},
		{
			name: "bad yaml",
			content: `---
name: [unterminated
---
body
`,
			err: "parse frontmatter",
		},
		{
			name: "unterminated frontmatter",
			content: `---
name: reviewer
`,
			err: "missing frontmatter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{"agents/reviewer.md": &fstest.MapFile{Data: []byte(tt.content)}}

			got, err := fs.AgentFile(fsys, "agents/reviewer.md")
			if tt.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.err)
				assert.Contains(t, err.Error(), "agents/reviewer.md", "errors name the file")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "reviewer", got.Name)
			assert.Equal(t, "Reviews code for defects", got.Description)
			assert.Equal(t, "mock/large", got.Model)
			assert.Equal(t, []string{"read", "grep"}, got.Tools)
			assert.Equal(t, "You review code.", got.Prompt)
			assert.Equal(t, "agents/reviewer.md", got.Source)
		})
	}
}

func TestAgentFileNameDefaultsToFilename(t *testing.T) {
	fsys := fstest.MapFS{"agents/reviewer.md": &fstest.MapFile{Data: []byte(`---
description: Reviews code
---
body
`)}}

	got, err := fs.AgentFile(fsys, "agents/reviewer.md")
	require.NoError(t, err)
	assert.Equal(t, "reviewer", got.Name)
}

func TestAgentFileToolsAsCommaString(t *testing.T) {
	fsys := fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte(`---
name: reviewer
tools: read, grep
---
body
`)}}

	got, err := fs.AgentFile(fsys, "a.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"read", "grep"}, got.Tools)
}

func TestAgentFileCarriesNoScope(t *testing.T) {
	// Scope is where a file sits relative to the root of a resolver. One
	// file read on its own has no way to know that. The resolver sets it.
	fsys := fstest.MapFS{"agents/apps/web/api.md": &fstest.MapFile{Data: []byte(`---
name: api
---
body
`)}}

	got, err := fs.AgentFile(fsys, "agents/apps/web/api.md")
	require.NoError(t, err)
	assert.Empty(t, got.Scope)
}

func TestMustAgentFilePanics(t *testing.T) {
	fsys := fstest.MapFS{}
	assert.Panics(t, func() { fs.MustAgentFile(fsys, "missing.md") })
}

// --- skills ---

func TestSkillDir(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/commit/SKILL.md": &fstest.MapFile{Data: []byte(`---
name: commit
description: Writes a commit message
---
Follow conventional commits.
`)},
	}

	got, err := fs.SkillDir(fsys, "skills/commit")
	require.NoError(t, err)
	assert.Equal(t, "commit", got.Name)
	assert.Equal(t, "Writes a commit message", got.Description)
	assert.Empty(t, got.Dir, "an io/fs path is not agent-reachable, so Dir stays empty")
	assert.Equal(t, "Follow conventional commits.", got.Body, "the body is read with the metadata")
}

func TestSkillDirReportsMalformedContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "bad yaml",
			content: "---\nname: [unterminated\n---\nbody\n",
		},
		{
			name:    "missing frontmatter",
			content: "Just a body.\n",
		},
		{
			name:    "unterminated frontmatter",
			content: "---\nname: commit\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"skills/commit/SKILL.md": &fstest.MapFile{Data: []byte(tt.content)},
			}

			// The tree resolvers skip a malformed file. A caller that
			// names one specific skill asked about that skill.
			_, err := fs.SkillDir(fsys, "skills/commit")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "skills/commit/SKILL.md", "the error names the file")
		})
	}
}

func TestSkillDirUnreadableFileIsAnError(t *testing.T) {
	// No file at all is not a broken skill. It is a resolver pointed at
	// the wrong place, and that stays loud.
	_, err := fs.SkillDir(fstest.MapFS{}, "skills/commit")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills/commit/SKILL.md")
}

func TestSkillDirNameDefaultsToDirName(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/commit/SKILL.md": &fstest.MapFile{Data: []byte(`---
description: Writes a commit message
---
body
`)},
	}

	got, err := fs.SkillDir(fsys, "skills/commit")
	require.NoError(t, err)
	assert.Equal(t, "commit", got.Name)
}
