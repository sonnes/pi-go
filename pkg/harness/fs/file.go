package fs

import (
	"fmt"
	iofs "io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sonnes/pi-go/pkg/harness/def"
)

// skillFile is the file a skill directory is defined by.
const skillFile = "SKILL.md"

// AgentFile reads one agent definition from a markdown file with YAML
// frontmatter: name, description, model, and tools in the block, the
// agent's prompt in the body.
//
// The name defaults to the file name without its extension, so a file
// at agents/reviewer.md needs no name key.
//
// It takes an [io/fs.FS] rather than a path so definitions can be
// embedded with [embed.FS] as easily as read from disk.
//
// A malformed file — no frontmatter block, an unterminated one, YAML
// that will not unmarshal — is an error here, because a caller naming
// one file asked about that file. [Agents], which walks a tree the user
// authors, skips such a file instead.
func AgentFile(fsys iofs.FS, filePath string) (def.Agent, error) {
	data, err := iofs.ReadFile(fsys, filePath)
	if err != nil {
		return def.Agent{}, fmt.Errorf("fs: read %s: %w", filePath, err)
	}

	meta, body, err := splitFrontmatter(data)
	if err != nil {
		return def.Agent{}, fmt.Errorf("fs: %s: %w: %w", filePath, errMalformed, err)
	}

	var fm agentMeta
	if err := yaml.Unmarshal(meta, &fm); err != nil {
		return def.Agent{}, fmt.Errorf(
			"fs: %s: %w: parse frontmatter: %w",
			filePath, errMalformed, err,
		)
	}

	name := fm.Name
	if name == "" {
		name = strings.TrimSuffix(path.Base(filePath), path.Ext(filePath))
	}

	return def.Agent{
		Name:        name,
		Description: fm.Description,
		Prompt:      string(body),
		Model:       fm.Model,
		Tools:       fm.Tools,
		Source:      filePath,
	}, nil
}

// MustAgentFile is [AgentFile] for definitions embedded at build time,
// where a malformed file is a programming error rather than something
// to handle. It panics on failure.
func MustAgentFile(fsys iofs.FS, filePath string) def.Agent {
	a, err := AgentFile(fsys, filePath)
	if err != nil {
		panic(err)
	}
	return a
}

// SkillDir reads one skill from a directory containing a SKILL.md. The
// whole file is read: frontmatter into the metadata, body into
// [def.Skill.Body]. The body stays out of the system prompt regardless
// — it reaches the conversation through the skill tool — so eagerness
// here costs prompt tokens nothing.
//
// The name defaults to the directory name.
//
// [def.Skill.Dir] is left empty: an [io/fs.FS] path is not something
// an agent can reach in general. [SkillsAt] resolves skills from a
// directory on disk and fills Dir with the absolute path.
//
// A malformed SKILL.md — no frontmatter block, an unterminated one,
// YAML that will not unmarshal — is an error here, because a caller
// naming one directory asked about that skill. [Skills], which walks a
// tree the user authors, skips such a file instead.
func SkillDir(fsys iofs.FS, dir string) (def.Skill, error) {
	filePath := path.Join(dir, skillFile)
	data, err := iofs.ReadFile(fsys, filePath)
	if err != nil {
		return def.Skill{}, fmt.Errorf("fs: read %s: %w", filePath, err)
	}

	meta, body, err := splitFrontmatter(data)
	if err != nil {
		return def.Skill{}, fmt.Errorf("fs: %s: %w: %w", filePath, errMalformed, err)
	}

	var fm skillMeta
	if err := yaml.Unmarshal(meta, &fm); err != nil {
		return def.Skill{}, fmt.Errorf(
			"fs: %s: %w: parse frontmatter: %w",
			filePath, errMalformed, err,
		)
	}

	name := fm.Name
	if name == "" {
		name = path.Base(dir)
	}

	return def.Skill{
		Name:        name,
		Description: fm.Description,
		Body:        string(body),
		Source:      filePath,
	}, nil
}
