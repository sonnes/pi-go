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
// description.md. A client registers the tool under any name. The client
// can pass this text, or its own text, as the description.
//
//go:embed description.md
var Description string

// ToolName is the default name for this tool in the registry.
const ToolName = "write"

// Input defines the parameters for the Write tool.
type Input struct {
	Path    string `json:"path" jsonschema:"Path to the file to write. Absolute, or relative to the working directory. Parent directories are created as needed."`
	Content string `json:"content" jsonschema:"Full contents to write. Any existing file at this path is overwritten."`
}

// fsys is the filesystem surface that the Write tool needs. The tool
// resolves a user path against the root of the filesystem. Then the tool
// creates the file and any parent directories that the file requires.
type fsys interface {
	fs.FS
	Resolve(path string) (string, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
}

// writer creates files on a confined filesystem. It holds only the
// tool's real dependency. Identity (name, description) belongs to the
// client that registers the tool.
type writer struct{ fsys fsys }

// Option configures a Write tool.
type Option interface{ apply(*writer) }

// FS sets the filesystem that the tool writes to, for example a
// sandbox.FS. The filesystem resolves and confines paths against its own
// root.
type FS struct{ FS fsys }

func (f FS) apply(w *writer) { w.fsys = f.FS }

// New returns the Write tool's runner. The runner writes a file and
// returns a confirmation. Pass the runner to ai.DefineTool with a name and
// description.
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
