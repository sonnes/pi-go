// Package edit provides the Edit tool for modifying files.
package edit

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	_ "embed"
)

// Description is the default tool documentation, embedded from
// description.md. Clients register the tool under any name and may pass
// this (or their own text) as the description.
//
//go:embed description.md
var Description string

// ToolName is the suggested default registry name for this tool.
const ToolName = "edit"

// Edit is a single targeted replacement. Each Edit is matched against the
// original file content, not applied incrementally.
type Edit struct {
	OldString string `json:"old_string" jsonschema:"Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].old_string in the same call."`
	NewString string `json:"new_string" jsonschema:"Replacement text for this targeted edit."`
}

// Input defines the parameters for the Edit tool. Provide either a single
// flat edit (old_string/new_string) or a batch (edits), but not both.
type Input struct {
	Path      string `json:"path" jsonschema:"Path to the file to edit. Absolute, or relative to the working directory."`
	OldString string `json:"old_string,omitempty" jsonschema:"Exact text to replace for a single edit. Must be unique in the file. Provide this with new_string instead of edits[] for one replacement."`
	NewString string `json:"new_string,omitempty" jsonschema:"Replacement text for the single old_string edit."`
	Edits     []Edit `json:"edits,omitempty" jsonschema:"A batch of targeted replacements as an alternative to old_string/new_string. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead."`
}

// edits returns the effective edit list, accepting either the flat
// old_string/new_string form or the batch edits[] form, but not both.
func (in Input) edits() ([]Edit, error) {
	flat := in.OldString != "" || in.NewString != ""
	switch {
	case flat && len(in.Edits) > 0:
		return nil, fmt.Errorf("provide either old_string/new_string or edits[], not both")
	case flat:
		return []Edit{{OldString: in.OldString, NewString: in.NewString}}, nil
	default:
		return in.Edits, nil
	}
}

// fsys is the filesystem surface the Edit tool needs: resolve a user path
// against its own root, read the existing content, then write the
// replacement back.
type fsys interface {
	fs.FS
	Resolve(path string) (string, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
}

// editor modifies files on a confined filesystem. It carries only the
// tool's real dependency — identity (name, description) belongs to the
// client that registers it.
type editor struct{ fsys fsys }

// Option configures an Edit tool.
type Option interface{ apply(*editor) }

// FS sets the filesystem the tool edits. It resolves and confines paths
// against its own root (e.g. a sandbox.FS).
type FS struct{ FS fsys }

func (f FS) apply(e *editor) { e.fsys = f.FS }

// New returns the Edit tool's runner: it applies targeted replacements to a
// file and returns a confirmation. Pass it to ai.DefineTool with a name and
// description.
func New(opts ...Option) func(context.Context, Input) (string, error) {
	e := &editor{}
	for _, o := range opts {
		o.apply(e)
	}
	return e.run
}

func (e *editor) run(_ context.Context, input Input) (string, error) {
	name, err := e.fsys.Resolve(input.Path)
	if err != nil {
		return "", err
	}

	edits, err := input.edits()
	if err != nil {
		return "", err
	}
	if len(edits) == 0 {
		return "", fmt.Errorf("edits cannot be empty")
	}

	original, err := fs.ReadFile(e.fsys, name)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	ending := detectLineEnding(string(original))
	bom, body := stripBOM(string(original))
	normalized := normalizeToLF(body)

	newNormalized, err := applyEdits(normalized, edits)
	if err != nil {
		return "", err
	}

	if newNormalized == normalized {
		return "", fmt.Errorf("no changes made: the edits produced identical content")
	}

	out := bom + restoreLineEndings(newNormalized, ending)
	if err := e.fsys.WriteFile(name, []byte(out), 0o644); err != nil { //nolint:gosec // Standard file permissions
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Successfully applied %d edit(s) to %s", len(edits), name), nil
}

// match is a located edit: where its old_string was found in the content
// and the new_string to splice in.
type match struct {
	index   int
	length  int
	newText string
}

// applyEdits validates and applies all edits against the same LF-normalized
// content. Each old_string must be present exactly once, and matches must
// not overlap. Replacements are applied in reverse order so earlier indices
// stay valid.
func applyEdits(content string, edits []Edit) (string, error) {
	matches := make([]match, 0, len(edits))
	for i, e := range edits {
		oldText := normalizeToLF(e.OldString)
		newText := normalizeToLF(e.NewString)
		if oldText == "" {
			return "", fmt.Errorf("edits[%d].old_string cannot be empty", i)
		}

		count := strings.Count(content, oldText)
		if count == 0 {
			return "", fmt.Errorf("edits[%d].old_string not found in file", i)
		}
		if count > 1 {
			return "", fmt.Errorf(
				"edits[%d].old_string appears %d times; each old_string must be unique, provide more surrounding context",
				i,
				count,
			)
		}

		matches = append(matches, match{
			index:   strings.Index(content, oldText),
			length:  len(oldText),
			newText: newText,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].index < matches[j].index
	})
	for i := 1; i < len(matches); i++ {
		if matches[i-1].index+matches[i-1].length > matches[i].index {
			return "", fmt.Errorf("edits overlap; merge them into one edit or target disjoint regions")
		}
	}

	// Apply in reverse so earlier match indices remain valid.
	out := content
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		out = out[:m.index] + m.newText + out[m.index+m.length:]
	}
	return out, nil
}

// detectLineEnding reports the dominant line ending of content: "\r\n" if
// the first newline is part of a CRLF, otherwise "\n".
func detectLineEnding(content string) string {
	crlf := strings.Index(content, "\r\n")
	lf := strings.IndexByte(content, '\n')
	if lf == -1 || crlf == -1 {
		return "\n"
	}
	if crlf < lf {
		return "\r\n"
	}
	return "\n"
}

// normalizeToLF collapses CRLF and lone CR into LF.
func normalizeToLF(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// restoreLineEndings converts LF back to ending across the whole text.
func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// stripBOM splits a leading UTF-8 BOM off content so it can be re-attached
// after editing.
func stripBOM(content string) (bom, text string) {
	const bomRune = "\uFEFF"
	if strings.HasPrefix(content, bomRune) {
		return bomRune, content[len(bomRune):]
	}
	return "", content
}
