package fs_test

import (
	"context"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/harness"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/fs"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
)

// --- agents and skills ---

func TestAgentsDir(t *testing.T) {
	fsys := fstest.MapFS{
		"defs/reviewer.md": &fstest.MapFile{Data: []byte("---\nname: reviewer\ndescription: Reviews\n---\nbody\n")},
		"defs/writer.md":   &fstest.MapFile{Data: []byte("---\nname: writer\ndescription: Writes\n---\nbody\n")},
		"defs/notes.txt":   &fstest.MapFile{Data: []byte("ignored")},
	}

	got, err := fs.Agents(fsys, "defs").Agents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2, "only markdown files count")
	assert.Equal(t, "reviewer", got[0].Name)
	assert.Equal(t, "writer", got[1].Name)
}

func TestAgentsInSubdirectoriesAreScoped(t *testing.T) {
	fsys := fstest.MapFS{
		"defs/reviewer.md":        &fstest.MapFile{Data: []byte("---\nname: reviewer\n---\nRoot.\n")},
		"defs/apps/web/api.md":    &fstest.MapFile{Data: []byte("---\nname: api\n---\nWeb.\n")},
		"defs/.hidden/nope.md":    &fstest.MapFile{Data: []byte("---\nname: nope\n---\nNo.\n")},
		"defs/apps/web/notes.txt": &fstest.MapFile{Data: []byte("ignored")},
	}

	got, err := fs.Agents(fsys, "defs").Agents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "api", got[0].Name)
	assert.Equal(t, "apps/web", got[0].Scope)
	assert.Equal(t, "reviewer", got[1].Name)
	assert.Equal(t, "", got[1].Scope)
}

func TestSkillsDir(t *testing.T) {
	fsys := fstest.MapFS{
		"lib/commit/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: commit\n---\nBody.\n")},
		"lib/notes/README.md": &fstest.MapFile{Data: []byte("no SKILL.md here")},
	}

	got, err := fs.Skills(fsys, "lib").Skills(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1, "a directory without a SKILL.md is skipped, not an error")
	assert.Equal(t, "commit", got[0].Name)
	assert.Empty(t, got[0].Dir, "an io/fs path is not agent-reachable, so Dir stays empty")
}

func TestSkillsAtSetsAbsoluteDir(t *testing.T) {
	root := writeTree(t, map[string]string{
		".agents/skills/commit/SKILL.md":          "---\nname: commit\n---\nBody.\n",
		".agents/skills/apps/web/deploy/SKILL.md": "---\nname: deploy\n---\nWeb.\n",
	})

	got, err := fs.SkillsAt(root, ".agents/skills").Skills(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Dir is a path the agent can reach — absolute, on disk.
	byName := map[string]def.Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	assert.Equal(t, filepath.Join(root, ".agents/skills/apps/web/deploy"), byName["deploy"].Dir)
	assert.Equal(t, "apps/web", byName["deploy"].Scope, "scoping works exactly like Skills")
	assert.Equal(t, "Body.", byName["commit"].Body)
}

func TestSkillsTreeSkipsBrokenSkills(t *testing.T) {
	fsys := fstest.MapFS{
		"lib/commit/SKILL.md":         &fstest.MapFile{Data: []byte("---\nname: commit\n---\nBody.\n")},
		"lib/bad-yaml/SKILL.md":       &fstest.MapFile{Data: []byte("---\nname: [unterminated\n---\nBody.\n")},
		"lib/no-frontmatter/SKILL.md": &fstest.MapFile{Data: []byte("Just a body.\n")},
		"lib/unterminated/SKILL.md":   &fstest.MapFile{Data: []byte("---\nname: commit\n")},
	}

	got, err := fs.Skills(fsys, "lib").Skills(context.Background())
	require.NoError(t, err, "a malformed file never fails the resolution")

	// The walk skips a malformed file outright. It does not degrade the
	// skill and does not list it.
	require.Len(t, got, 1)
	assert.Equal(t, "commit", got[0].Name)
	for _, s := range got {
		assert.NotContains(t, s.Description, "(broken:")
		assert.NotContains(t, s.Body, "(broken:")
	}
}

func TestSkillsAtSkipsBrokenSkills(t *testing.T) {
	root := writeTree(t, map[string]string{
		".agents/skills/commit/SKILL.md": "---\nname: commit\n---\nBody.\n",
		".agents/skills/broken/SKILL.md": "---\nname: [unterminated\n---\nBody.\n",
	})

	got, err := fs.SkillsAt(root, ".agents/skills").Skills(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "commit", got[0].Name)
}

func TestBrokenSkillStillOwnsItsSubtree(t *testing.T) {
	fsys := fstest.MapFS{
		"lib/broken/SKILL.md":          &fstest.MapFile{Data: []byte("---\nname: [unterminated\n---\nB.\n")},
		"lib/broken/examples/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: sample\n---\nA sample.\n")},
	}

	got, err := fs.Skills(fsys, "lib").Skills(context.Background())
	require.NoError(t, err)

	// A skip of the broken skill must not turn its support files into
	// skills of their own. A skill owns its directory either way.
	assert.Empty(t, got)
}

func TestAgentsTreeSkipsBrokenDefinitions(t *testing.T) {
	fsys := fstest.MapFS{
		"defs/reviewer.md":     &fstest.MapFile{Data: []byte("---\nname: reviewer\n---\nRoot.\n")},
		"defs/bad-yaml.md":     &fstest.MapFile{Data: []byte("---\nname: [unterminated\n---\nbody\n")},
		"defs/no-front.md":     &fstest.MapFile{Data: []byte("Just a body.\n")},
		"defs/unterminated.md": &fstest.MapFile{Data: []byte("---\nname: writer\n")},
	}

	// In an earlier version this failed the whole resolution, so every
	// session that shared the directory died on one bad file.
	got, err := fs.Agents(fsys, "defs").Agents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "reviewer", got[0].Name)
}

func TestMalformedFileDoesNotStopLaterSiblings(t *testing.T) {
	ctx := context.Background()

	// "a-broken" sorts before "b-good". The walk continues past the skip
	// and does not stop there.
	agents := fstest.MapFS{
		"defs/a-broken.md": &fstest.MapFile{Data: []byte("---\nname: [unterminated\n---\nbody\n")},
		"defs/b-good.md":   &fstest.MapFile{Data: []byte("---\nname: writer\n---\nbody\n")},
	}
	gotAgents, err := fs.Agents(agents, "defs").Agents(ctx)
	require.NoError(t, err)
	require.Len(t, gotAgents, 1)
	assert.Equal(t, "writer", gotAgents[0].Name)

	skills := fstest.MapFS{
		"lib/a-broken/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: [unterminated\n---\nB.\n")},
		"lib/b-good/SKILL.md":   &fstest.MapFile{Data: []byte("---\nname: release\n---\nB.\n")},
	}
	gotSkills, err := fs.Skills(skills, "lib").Skills(ctx)
	require.NoError(t, err)
	require.Len(t, gotSkills, 1)
	assert.Equal(t, "release", gotSkills[0].Name)
}

func TestUnreadableFilesStillFailTheWalk(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("permission denied")

	// Malformed content is an authoring mistake, and the walk skips it.
	// An I/O error is not an authoring mistake, and the walk must never
	// swallow it.
	agents := unreadable{
		FS:   fstest.MapFS{"defs/reviewer.md": &fstest.MapFile{Data: []byte("---\nname: reviewer\n---\nb\n")}},
		path: "defs/reviewer.md",
		err:  boom,
	}
	_, err := fs.Agents(agents, "defs").Agents(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)

	skills := unreadable{
		FS:   fstest.MapFS{"lib/commit/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: commit\n---\nb\n")}},
		path: "lib/commit/SKILL.md",
		err:  boom,
	}
	_, err = fs.Skills(skills, "lib").Skills(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

// --- scope from the directory tree ---

func TestSkillsInSubdirectoriesAreScoped(t *testing.T) {
	fsys := fstest.MapFS{
		"lib/deploy/SKILL.md":          &fstest.MapFile{Data: []byte("---\nname: deploy\n---\nRoot.\n")},
		"lib/apps/web/deploy/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: deploy\n---\nWeb.\n")},
		"lib/apps/api/deploy/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: deploy\n---\nAPI.\n")},
	}

	got, err := fs.Skills(fsys, "lib").Skills(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 3)

	// The name stays as authored. The subdirectory it sits in becomes the
	// scope, which is what the harness qualifies the name with.
	bySource := map[string]def.Skill{}
	for _, s := range got {
		bySource[s.Source] = s
	}

	assert.Equal(t, "", bySource["lib/deploy/SKILL.md"].Scope, "a skill at the root of the tree has no scope")
	assert.Equal(t, "deploy", bySource["lib/deploy/SKILL.md"].Name)
	assert.Equal(t, "apps/web", bySource["lib/apps/web/deploy/SKILL.md"].Scope)
	assert.Equal(t, "deploy", bySource["lib/apps/web/deploy/SKILL.md"].Name)
	assert.Equal(t, "apps/api", bySource["lib/apps/api/deploy/SKILL.md"].Scope)
}

func TestSkillSupportDirectoriesAreNotSkills(t *testing.T) {
	fsys := fstest.MapFS{
		"lib/deploy/SKILL.md":              &fstest.MapFile{Data: []byte("---\nname: deploy\n---\nBody.\n")},
		"lib/deploy/examples/SKILL.md":     &fstest.MapFile{Data: []byte("---\nname: sample\n---\nA sample.\n")},
		"lib/deploy/scripts/validate.sh":   &fstest.MapFile{Data: []byte("#!/bin/sh\n")},
		"lib/.hidden/secret/SKILL.md":      &fstest.MapFile{Data: []byte("---\nname: secret\n---\nNo.\n")},
		"lib/notes/README.md":              &fstest.MapFile{Data: []byte("not a skill")},
		"lib/notes/nested/deploy/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: deploy\n---\nDeep.\n")},
	}

	got, err := fs.Skills(fsys, "lib").Skills(context.Background())
	require.NoError(t, err)

	// A skill owns everything below it, so its own files never become
	// skills. The walk skips a dot-directory outright.
	require.Len(t, got, 2)
	assert.Equal(t, "lib/deploy/SKILL.md", got[0].Source)
	assert.Equal(t, "lib/notes/nested/deploy/SKILL.md", got[1].Source)
	assert.Equal(t, "notes/nested", got[1].Scope)
}

func TestScopedSkillsGetQualifiedNames(t *testing.T) {
	fsys := os.DirFS(writeTree(t, map[string]string{
		".agents/skills/deploy/SKILL.md":          "---\nname: deploy\ndescription: root\n---\nbody\n",
		".agents/skills/apps/web/deploy/SKILL.md": "---\nname: deploy\ndescription: web\n---\nbody\n",
	}))

	env := envFor(t, harness.WithSkills(fs.Skills(fsys, ".agents/skills")))

	// Same base name, different directories: both survive, and the scoped
	// one carries the directory it governs in its name.
	require.Len(t, env.Skills, 2)
	assert.Equal(t, "apps/web:deploy", env.Skills[0].Name)
	assert.Equal(t, "deploy", env.Skills[1].Name)
}

// --- instructions ---

func TestInstructionsWalksTheTree(t *testing.T) {
	fsys := os.DirFS(writeTree(t, map[string]string{
		"AGENTS.md":            "Root rules.\n",
		"web/AGENTS.md":        "Web rules.\n",
		"web/api/AGENTS.md":    "API rules.\n",
		"web/README.md":        "not an AGENTS.md",
		".hidden/AGENTS.md":    "Skipped.\n",
		"docs/nothing-here.md": "no AGENTS.md in this directory",
	}))

	got, err := fs.Instructions(fsys, "AGENTS.md").Instructions(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 3, "one per directory that has one; dot-directories are skipped")

	// The root document governs the whole tree, so it carries no Dir. A
	// nested one is bound to the directory it sits in. Lexical order puts
	// the root first.
	assert.Equal(t, "", got[0].Dir)
	assert.Equal(t, "Root rules.", got[0].Content)
	assert.Equal(t, "web", got[1].Dir)
	assert.Equal(t, "web/AGENTS.md", got[1].Source)
	assert.Equal(t, "web/api", got[2].Dir)
}

func TestInstructionsFileReadsExactlyOneFile(t *testing.T) {
	fsys := os.DirFS(writeTree(t, map[string]string{
		"AGENTS.md":     "Root rules.\n",
		"web/AGENTS.md": "Web rules.\n",
	}))
	ctx := context.Background()

	got, err := fs.InstructionsFile(fsys, "AGENTS.md").Instructions(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1, "the named file only — no walk")
	assert.Equal(t, "Root rules.", got[0].Content)
	assert.Equal(t, "AGENTS.md", got[0].Source)
	assert.Empty(t, got[0].Dir)

	missing, err := fs.InstructionsFile(fsys, "missing.md").Instructions(ctx)
	require.NoError(t, err, "a missing file resolves to nothing, like a missing root")
	assert.Empty(t, missing)
}

func TestOnlyUnboundInstructionsReachThePrompt(t *testing.T) {
	fsys := os.DirFS(writeTree(t, map[string]string{
		"AGENTS.md":     "Root rules.\n",
		"web/AGENTS.md": "Web rules.\n",
	}))

	env := envFor(t, harness.WithInstructions(fs.Instructions(fsys, "AGENTS.md")))

	// The harness resolves both and hands them to the builder. What
	// prompt.Default does with a bound one is its business, not the
	// business of the resolver.
	require.Len(t, env.Instructions, 2)
	assert.Equal(t, "", env.Instructions[0].Dir)
	assert.Equal(t, "web", env.Instructions[1].Dir)

	sys, err := prompt.Default(context.Background(), env)
	require.NoError(t, err)
	assert.Contains(t, sys, "Root rules.")
	assert.NotContains(t, sys, "Web rules.", "a directory-bound document is left to the caller")
}

// --- absence ---

func TestResolversTolerateAbsence(t *testing.T) {
	fsys := os.DirFS(t.TempDir())
	ctx := context.Background()

	agents, err := fs.Agents(fsys, "nope").Agents(ctx)
	require.NoError(t, err)
	assert.Empty(t, agents)

	skills, err := fs.Skills(fsys, "nope").Skills(ctx)
	require.NoError(t, err)
	assert.Empty(t, skills)

	docs, err := fs.Instructions(fsys, "nope.md").Instructions(ctx)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestResolversTolerateAMissingRoot(t *testing.T) {
	fsys := os.DirFS("/no/such/directory")
	ctx := context.Background()

	agents, err := fs.Agents(fsys, "agents").Agents(ctx)
	require.NoError(t, err)
	assert.Empty(t, agents)

	skills, err := fs.Skills(fsys, "skills").Skills(ctx)
	require.NoError(t, err)
	assert.Empty(t, skills)

	docs, err := fs.Instructions(fsys, "AGENTS.md").Instructions(ctx)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

// --- embedded artifacts ---

func TestResolversOverEmbed(t *testing.T) {
	sub, err := iofs.Sub(builtin, "testdata")
	require.NoError(t, err)
	ctx := context.Background()

	agents, err := fs.Agents(sub, ".agents/agents").Agents(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "embedded", agents[0].Name)
	assert.Equal(t, "You were compiled in.", agents[0].Prompt)

	skills, err := fs.Skills(sub, ".agents/skills").Skills(ctx)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	assert.Equal(t, "release", skills[0].Name)
	assert.Equal(t, "Tag, then publish.", skills[0].Body)

	docs, err := fs.Instructions(sub, "AGENTS.md").Instructions(ctx)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, "Built-in house rules.", docs[0].Content)
}

// --- hierarchies ---

func TestHierarchyOfRoots(t *testing.T) {
	home := os.DirFS(writeTree(t, map[string]string{
		".agents/agents/reviewer.md":      "---\nname: reviewer\ndescription: home\n---\nbody\n",
		".agents/agents/writer.md":        "---\nname: writer\ndescription: home\n---\nbody\n",
		".agents/skills/commit/SKILL.md":  "---\nname: commit\ndescription: home\n---\nbody\n",
		".agents/skills/release/SKILL.md": "---\nname: release\ndescription: home\n---\nbody\n",
		"AGENTS.md":                       "Home rules.\n",
	}))
	project := os.DirFS(writeTree(t, map[string]string{
		".agents/agents/reviewer.md":     "---\nname: reviewer\ndescription: project\n---\nbody\n",
		".agents/skills/commit/SKILL.md": "---\nname: commit\ndescription: project\n---\nbody\n",
		"AGENTS.md":                      "Project rules.\n",
	}))

	// A hierarchy is a list of roots, lowest first.
	env := envFor(
		t,
		harness.WithAgents(
			fs.Agents(home, ".agents/agents"),
			fs.Agents(project, ".agents/agents"),
		),
		harness.WithSkills(
			fs.Skills(home, ".agents/skills"),
			fs.Skills(project, ".agents/skills"),
		),
		harness.WithInstructions(
			fs.Instructions(home, "AGENTS.md"),
			fs.Instructions(project, "AGENTS.md"),
		),
	)

	require.Len(t, env.Agents, 2)
	assert.Equal(t, "reviewer", env.Agents[0].Name)
	assert.Equal(t, "project", env.Agents[0].Description, "the later root replaces the earlier one")
	assert.Equal(t, "writer", env.Agents[1].Name, "a name only the lower root has survives")

	require.Len(t, env.Skills, 2)
	assert.Equal(t, "commit", env.Skills[0].Name)
	assert.Equal(t, "project", env.Skills[0].Description)
	assert.Equal(t, "release", env.Skills[1].Name)

	require.Len(t, env.Instructions, 2, "instructions concatenate across roots")
	assert.Equal(t, "Home rules.", env.Instructions[0].Content)
	assert.Equal(t, "Project rules.", env.Instructions[1].Content)
}

func TestPerBuildRootAppendsToTheBaseline(t *testing.T) {
	// The baseline carries a name the project does not. An append and a
	// replacement are therefore distinguishable, because a replacement
	// removes "release".
	baseline := os.DirFS(writeTree(t, map[string]string{
		".agents/skills/commit/SKILL.md":  "---\nname: commit\ndescription: baseline\n---\nbody\n",
		".agents/skills/release/SKILL.md": "---\nname: release\ndescription: baseline\n---\nbody\n",
	}))
	perBuild := os.DirFS(writeTree(t, map[string]string{
		".agents/skills/commit/SKILL.md": "---\nname: commit\ndescription: per-build\n---\nbody\n",
	}))

	var captured *prompt.Env
	h, err := harness.New(
		harness.WithCatalog(testCatalog(&mockProvider{})),
		harness.WithDefaultModel("mock/small"),
		harness.WithSkills(fs.Skills(baseline, ".agents/skills")),
		harness.WithPromptBuilder(captureEnv(&captured)),
	)
	require.NoError(t, err)
	ctx := context.Background()

	// One harness, many repositories. The root of the project arrives
	// when the session is minted, as resources on top of what the process
	// knows.
	_, err = h.Agent(ctx, harness.WithSkills(fs.Skills(perBuild, ".agents/skills")))
	require.NoError(t, err)

	require.Len(t, captured.Skills, 2)
	assert.Equal(t, "per-build", captured.Skills[0].Description, "the project wins the name")
	assert.Equal(t, "release", captured.Skills[1].Name)
	assert.Equal(t, "baseline", captured.Skills[1].Description, "and the rest of the baseline stands")

	// A build that adds nothing sees the baseline alone.
	_, err = h.Agent(ctx)
	require.NoError(t, err)
	require.Len(t, captured.Skills, 2)
	assert.Equal(t, "baseline", captured.Skills[0].Description)
}
