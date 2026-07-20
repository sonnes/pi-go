// Package find provides the find tool for locating files by glob pattern.
package find

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	_ "embed"
)

// Description is the default tool documentation, embedded from
// description.md. Clients register the tool under any name and may pass
// this (or their own text) as the description.
//
//go:embed description.md
var Description string

// ToolName is the suggested default registry name for this tool.
const ToolName = "find"

// DefaultLimit is the default maximum number of results returned.
const DefaultLimit = 1000

// Input defines the parameters for the find tool.
type Input struct {
	Pattern string `json:"pattern" jsonschema:"Glob pattern to match file paths, e.g. \"**/*.go\"."`
	Path    string `json:"path,omitempty" jsonschema:"Directory to search under. Absolute, or relative to the working directory. Omit to search from the working directory root."`
	Limit   *int   `json:"limit,omitempty" jsonschema:"Maximum number of file paths to return."`
}

// fsys is the filesystem the find tool searches: it walks files, resolves
// the search base against its own root, and reports that root for
// user-facing paths.
type fsys interface {
	fs.FS
	ResolveDir(path string) (string, error)
	Root() string
}

// finder searches a confined filesystem. It carries only the tool's real
// dependency — identity (name, description) belongs to the client that
// registers it.
type finder struct{ fsys fsys }

// Option configures a find tool.
type Option interface{ apply(*finder) }

// FS sets the filesystem the tool searches. It resolves and confines
// paths against its own root (e.g. a sandbox.FS).
type FS struct{ FS fsys }

func (f FS) apply(fd *finder) { fd.fsys = f.FS }

// New returns the find tool's runner: it locates files by glob pattern and
// returns newline-separated paths (or a message when none match). Pass it
// to [ai.DefineTool]/[ai.DefineParallelTool] with a name and description.
func New(opts ...Option) func(context.Context, Input) (string, error) {
	f := &finder{}
	for _, o := range opts {
		o.apply(f)
	}
	return f.run
}

func (f *finder) run(_ context.Context, input Input) (string, error) {
	base, err := f.fsys.ResolveDir(input.Path)
	if err != nil {
		return "", err
	}

	fsys, err := fs.Sub(f.fsys, base)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", displayPath(f.fsys.Root(), base))
	}

	info, err := fs.Stat(fsys, ".")
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", displayPath(f.fsys.Root(), base))
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", displayPath(f.fsys.Root(), base))
	}

	matches, err := doublestar.Glob(fsys, input.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid glob pattern: %w", err)
	}

	files := make([]string, 0, len(matches))
	for _, match := range matches {
		matchInfo, statErr := fs.Stat(fsys, match)
		if statErr != nil || matchInfo.IsDir() {
			continue
		}
		files = append(files, path.Join(base, match))
	}

	if len(files) == 0 {
		return "No files found", nil
	}

	limit := DefaultLimit
	if input.Limit != nil && *input.Limit > 0 {
		limit = *input.Limit
	}

	truncated := false
	if len(files) > limit {
		files = files[:limit]
		truncated = true
	}

	out := strings.Join(files, "\n")
	if truncated {
		out += fmt.Sprintf("\n... (results truncated to %d)", limit)
	}
	return out, nil
}

func displayPath(workingDir, base string) string {
	if workingDir == "" {
		workingDir = "."
	}
	if base == "." {
		return workingDir
	}
	return filepath.Join(workingDir, filepath.FromSlash(base))
}
