package fs

import (
	"context"
	"errors"
	iofs "io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/sonnes/pi-go/pkg/harness/def"
)

// Agents returns a resolver over every markdown file in the tree under
// dir, one agent definition per file. The walk ignores files that are
// not .md and skips dot-directories. A missing directory resolves to
// nothing rather than an error: a convention nobody has adopted yet is
// not a mistake in the configuration.
//
// The walk skips a malformed file, and it does so silently. A definition
// a user is halfway through must not fail every session that shares the
// directory. A file that cannot be read at all — permissions, I/O — is
// still an error, because authoring cannot fix that. If a definition
// does not appear, read it with [AgentFile], which reports what the walk
// passed over.
//
// A file in a subdirectory is scoped to it. So agents/review/api.md is
// named "api" with a [def.Agent.Scope] of "review", which the harness
// qualifies to "review:api". A subdirectory is therefore a namespace:
// two teams can both have an "api" agent.
//
// It takes an [io/fs.FS] rather than a path. Definitions in an [embed.FS]
// then resolve exactly like definitions on disk:
//
//	fs.Agents(os.DirFS(dir), "agents")
//	fs.Agents(builtin, "defs/agents")   // an embed.FS
func Agents(fsys iofs.FS, dir string) def.AgentResolver {
	return agentDir{fsys: fsys, dir: dir}
}

// Skills returns a resolver over every directory in the tree under dir
// that holds a SKILL.md. The walk skips a directory without one rather
// than reports it, because a skill library grows support files. The walk
// skips dot-directories too, and a missing dir resolves to nothing.
//
// It takes an [io/fs.FS], so the same call serves a directory on disk
// and skills compiled into the binary:
//
//	fs.Skills(os.DirFS(dir), "skills")
//	fs.Skills(builtin, "defs/skills")   // an embed.FS
//
// A skill owns everything below it. The walk does not descend past a
// SKILL.md, so the examples/ or scripts/ of a skill never become skills
// themselves. That holds for a skill whose SKILL.md did not parse too.
//
// The walk skips a malformed SKILL.md, and it does so silently. One bad
// file must not fail every session that shares the tree. A file that
// cannot be read at all — permissions, I/O — is still an error, because
// authoring cannot fix that. If a skill does not appear, read it with
// [SkillDir], which reports what the walk passed over.
//
// The directories that hold a skill are its namespace. A skill at
// skills/apps/web/deploy is named "deploy" with a [def.Skill.Scope] of
// "apps/web". The harness qualifies that to "apps/web:deploy", so it
// coexists with a "deploy" at the root and does not collide with it.
//
// Skills leaves [def.Skill.Dir] empty, because an agent cannot reach an
// [io/fs.FS] path in general. For a tree on disk, use [SkillsAt], which
// fills Dir with the absolute path.
func Skills(fsys iofs.FS, dir string) def.SkillResolver {
	return skillTree{fsys: fsys, dir: dir}
}

// SkillsAt is [Skills] over a directory on disk: root is an on-disk
// path, dir the subdirectory to scan within it. The agent can reach that
// tree, so SkillsAt sets [def.Skill.Dir] on each skill to the absolute
// path of its directory. The skill tool can then point the model at the
// support files of the skill.
func SkillsAt(root, dir string) def.SkillResolver {
	return skillTree{fsys: os.DirFS(root), dir: dir, diskRoot: root}
}

// Instructions returns a resolver over every file named name in the
// tree — the AGENTS.md convention, where a project writes one document
// at its root and another in any directory that needs its own rules:
//
//	fs.Instructions(os.DirFS(dir), "AGENTS.md")
//
// The root document carries no [def.Instructions.Dir]. A nested document
// is bound to the directory it sits in. The walk skips dot-directories,
// so a .git full of hooks costs nothing. A tree with no such file
// resolves to nothing rather than an error.
//
// The walk visits the entire tree on every build — O(repository),
// node_modules included. If only the root document matters, use
// [InstructionsFile], which reads one file and walks nothing.
//
// The caller decides what happens to a bound document. [prompt.Default]
// writes only the unbound ones into the prompt. A nested document is
// therefore discovered and handed to your builder, and no session pays
// for it. The package documentation explains why the harness does not
// decide this for you.
func Instructions(fsys iofs.FS, name string) def.InstructionResolver {
	return instructionTree{fsys: fsys, name: name}
}

// InstructionsFile returns a resolver over exactly one file. There is no
// walk, so it costs one read however large the tree around it is:
//
//	fs.InstructionsFile(os.DirFS(repo), "AGENTS.md")
//
// A missing file resolves to zero documents rather than an error. That is
// the convention of this package for a convention nobody adopted. The
// document carries no [def.Instructions.Dir].
func InstructionsFile(fsys iofs.FS, filePath string) def.InstructionResolver {
	return instructionFile{fsys: fsys, path: filePath}
}

// agentDir resolves agent definitions from a tree of markdown files.
type agentDir struct {
	fsys iofs.FS
	dir  string
}

func (d agentDir) Agents(context.Context) ([]def.Agent, error) {
	var out []def.Agent

	err := walk(d.fsys, d.dir, func(file string, entry iofs.DirEntry) error {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		a, err := AgentFile(d.fsys, file)
		switch {
		case errors.Is(err, errMalformed):
			return nil // one bad file is not everyone's problem
		case err != nil:
			return err
		}
		a.Scope = scopeOf(d.dir, path.Dir(file))
		out = append(out, a)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// skillTree resolves skills from a tree of skill directories. A
// non-empty diskRoot marks the tree as reachable on disk. Dir can then
// carry an absolute path.
type skillTree struct {
	fsys     iofs.FS
	dir      string
	diskRoot string
}

func (t skillTree) Skills(context.Context) ([]def.Skill, error) {
	var out []def.Skill

	err := walk(t.fsys, t.dir, func(dir string, entry iofs.DirEntry) error {
		if !entry.IsDir() {
			return nil
		}
		// The read is the test for presence. No SKILL.md means this is
		// an ordinary directory, so the walk continues down. A directory
		// that has one belongs to a skill whether or not the file
		// parsed, so the walk prunes the subtree either way. Support
		// files never become skills of their own.
		s, err := SkillDir(t.fsys, dir)
		switch {
		case errors.Is(err, iofs.ErrNotExist):
			return nil
		case errors.Is(err, errMalformed):
			return iofs.SkipDir // one bad file is not everyone's problem
		case err != nil:
			return err
		}
		s.Scope = scopeOf(t.dir, path.Dir(dir))
		if t.diskRoot != "" {
			s.Dir = filepath.Join(t.diskRoot, dir)
		}
		out = append(out, s)
		return iofs.SkipDir
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// walk visits every entry under root. It skips dot-directories and
// treats a missing root as an empty one. A visit that returns
// [io/fs.SkipDir] prunes that directory, which is how a skill claims
// everything below it.
func walk(fsys iofs.FS, root string, visit func(p string, e iofs.DirEntry) error) error {
	err := iofs.WalkDir(fsys, root, func(p string, e iofs.DirEntry, err error) error {
		switch {
		case errors.Is(err, iofs.ErrNotExist):
			return iofs.SkipAll // no tree at all is no artifacts
		case err != nil:
			return err
		}
		if e.IsDir() && p != root && strings.HasPrefix(path.Base(p), ".") {
			return iofs.SkipDir
		}
		return visit(p, e)
	})
	if errors.Is(err, iofs.ErrNotExist) {
		return nil
	}
	return err
}

// scopeOf reports where dir sits relative to the root of the resolver,
// which is the namespace an artifact there belongs to. The root itself
// is no namespace at all.
func scopeOf(root, dir string) string {
	if dir == root {
		return ""
	}
	return strings.TrimPrefix(dir, root+"/")
}

// instructionFile resolves a single instructions document from one
// known path.
type instructionFile struct {
	fsys iofs.FS
	path string
}

func (f instructionFile) Instructions(context.Context) ([]def.Instructions, error) {
	data, err := iofs.ReadFile(f.fsys, f.path)
	switch {
	case errors.Is(err, iofs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}

	doc := def.Instructions{
		Source:  f.path,
		Content: strings.TrimSpace(string(data)),
	}
	return []def.Instructions{doc}, nil
}

// instructionTree resolves the documents named name from a whole tree,
// one per directory that has one.
type instructionTree struct {
	fsys iofs.FS
	name string
}

func (t instructionTree) Instructions(context.Context) ([]def.Instructions, error) {
	var out []def.Instructions

	// WalkDir visits in lexical order, so the root document comes first
	// and the nested ones follow in a stable order.
	err := walk(t.fsys, ".", func(dir string, entry iofs.DirEntry) error {
		if !entry.IsDir() {
			return nil
		}

		file := path.Join(dir, t.name)
		data, err := iofs.ReadFile(t.fsys, file)
		switch {
		case errors.Is(err, iofs.ErrNotExist):
			return nil
		case err != nil:
			return err
		}

		out = append(out, def.Instructions{
			Dir:     scopeOf(".", dir),
			Source:  file,
			Content: strings.TrimSpace(string(data)),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
