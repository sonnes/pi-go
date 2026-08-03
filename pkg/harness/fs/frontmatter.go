package fs

import (
	"bytes"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// errNoFrontmatter is returned when a file does not open with a YAML
// frontmatter block. Callers wrap it with the file path.
var errNoFrontmatter = errors.New("missing frontmatter")

// errMalformed marks a file the package could read but not understand —
// no frontmatter block, an unterminated one, or YAML that will not
// unmarshal. It is the line between a user's authoring mistake, which
// the tree resolvers skip, and an I/O failure, which they report.
//
// Unexported: callers classify by behavior (the resolver skipped it),
// not by inspecting the error.
var errMalformed = errors.New("malformed")

// fence delimits a frontmatter block.
const fence = "---"

// splitFrontmatter separates a leading YAML frontmatter block from the
// document body. A file must open with a fence line and close the block
// with another one; anything else is [errNoFrontmatter].
func splitFrontmatter(data []byte) (meta, body []byte, err error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, fence+"\n") {
		return nil, nil, errNoFrontmatter
	}
	rest := text[len(fence)+1:]

	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return nil, nil, errNoFrontmatter
	}
	meta = []byte(rest[:end])

	after := rest[end+len(fence)+1:]
	after = strings.TrimPrefix(after, "\n")
	return meta, bytes.TrimSpace([]byte(after)), nil
}

// stringList decodes a YAML value that is either a sequence of strings
// or a single comma-separated string. Both spellings show up in the
// agent files people already write, and neither is worth an error.
type stringList []string

func (s *stringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		var items []string
		if err := node.Decode(&items); err != nil {
			return err
		}
		*s = items
		return nil
	case yaml.ScalarNode:
		var raw string
		if err := node.Decode(&raw); err != nil {
			return err
		}
		var items []string
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				items = append(items, part)
			}
		}
		*s = items
		return nil
	default:
		return errors.New("expected a list or a comma-separated string")
	}
}

// agentMeta is the frontmatter of an agent file.
type agentMeta struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Model       string     `yaml:"model"`
	Tools       stringList `yaml:"tools"`
}

// skillMeta is the frontmatter of a SKILL.md.
type skillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}
