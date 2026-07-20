// Package write provides the Write tool for creating files.
package write

import (
	"context"
	"fmt"
	"io/fs"
	"path"

	_ "embed"
)

// Description is the default tool documentation, embedded from
// description.md. Clients register the tool under any name and may pass
// this (or their own text) as the description.
//
//go:embed description.md
var Description string

// ToolName is the suggested default registry name for this tool.
const ToolName = "write"

// Input defines the parameters for the Write tool.
type Input struct {
	Path    string `json:"path" jsonschema:"Path to the file to write. Absolute, or relative to the working directory. Parent directories are created as needed."`
	Content string `json:"content" jsonschema:"Full contents to write. Any existing file at this path is overwritten."`
}

// fsys is the filesystem surface the Write tool needs: resolve a user
// path against its own root, then create the file and any parent
// directories it requires.
type fsys interface {
	fs.FS
	Resolve(path string) (string, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
}

// writer creates files on a confined filesystem. It carries only the
// tool's real dependency — identity (name, description) belongs to the
// client that registers it.
type writer struct{ fsys fsys }

// Option configures a Write tool.
type Option interface{ apply(*writer) }

// FS sets the filesystem the tool writes to. It resolves and confines
// paths against its own root (e.g. a sandbox.FS).
type FS struct{ FS fsys }

func (f FS) apply(w *writer) { w.fsys = f.FS }

// New returns the Write tool's runner: it writes a file and returns a
// confirmation. Pass it to ai.DefineTool with a name and description.
func New(opts ...Option) func(context.Context, Input) (string, error) {
	w := &writer{}
	for _, o := range opts {
		o.apply(w)
	}
	return w.run
}

func (w *writer) run(_ context.Context, input Input) (string, error) {
	name, err := w.fsys.Resolve(input.Path)
	if err != nil {
		return "", err
	}

	if dir := path.Dir(name); dir != "." {
		if err := w.fsys.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // Standard directory permissions
			return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := w.fsys.WriteFile(name, []byte(input.Content), 0o644); err != nil { //nolint:gosec // Standard file permissions
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(input.Content), name), nil
}
