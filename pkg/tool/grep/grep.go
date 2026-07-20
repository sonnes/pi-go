// Package grep provides the grep tool for searching file contents.
package grep

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
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
const ToolName = "grep"

// DefaultLimit is the default maximum number of matching lines returned.
const DefaultLimit = 100

// Input defines the parameters for the grep tool.
type Input struct {
	Context    *int   `json:"context,omitempty" jsonschema:"Number of context lines to show around each match."`
	Limit      *int   `json:"limit,omitempty" jsonschema:"Maximum number of matching lines to return."`
	Pattern    string `json:"pattern" jsonschema:"Regular expression to search for."`
	Path       string `json:"path,omitempty" jsonschema:"File or directory to search. Absolute, or relative to the working directory. Omit to search from the working directory root."`
	Glob       string `json:"glob,omitempty" jsonschema:"Glob filter limiting which files are searched, e.g. \"*.go\"."`
	IgnoreCase bool   `json:"ignore_case,omitempty" jsonschema:"Match case-insensitively."`
	Literal    bool   `json:"literal,omitempty" jsonschema:"Treat the pattern as a literal string instead of a regular expression."`
}

// fsys is the filesystem the grep tool searches: it walks files and
// resolves the search base against its own root.
type fsys interface {
	fs.FS
	ResolveDir(path string) (string, error)
}

// rootFS is optionally implemented by an FS backed by a real OS directory.
// When the configured FS implements it, grep shells out to ripgrep against
// that directory for speed; otherwise it falls back to a pure-Go search.
type rootFS interface {
	Root() string
}

// grepper searches a confined filesystem. It carries only the tool's real
// dependency — identity (name, description) belongs to the client that
// registers it.
type grepper struct{ fsys fsys }

// Option configures a grep tool.
type Option interface{ apply(*grepper) }

// FS sets the filesystem the tool searches. It resolves and confines
// paths against its own root (e.g. a sandbox.FS).
type FS struct{ FS fsys }

func (f FS) apply(g *grepper) { g.fsys = f.FS }

// New returns the grep tool's runner: it searches file contents by regexp
// and returns matching lines as file:line:text (or a message when none
// match). Pass it to [ai.DefineTool]/[ai.DefineParallelTool] with a name
// and description.
func New(opts ...Option) func(context.Context, Input) (string, error) {
	f := &grepper{}
	for _, o := range opts {
		o.apply(f)
	}
	return f.run
}

func (f *grepper) run(ctx context.Context, input Input) (string, error) {
	base, err := f.fsys.ResolveDir(input.Path)
	if err != nil {
		return "", err
	}

	root := ""
	if r, ok := f.fsys.(rootFS); ok {
		root = r.Root()
	}

	info, statErr := fs.Stat(f.fsys, base)
	if statErr != nil {
		return "", fmt.Errorf("path does not exist: %s", displayPath(root, base))
	}

	if root != "" {
		if rg, lookErr := exec.LookPath("rg"); lookErr == nil {
			return runRipgrep(ctx, rg, root, base, info.IsDir(), input)
		}
	}

	return runPureGo(f.fsys, base, info.IsDir(), input)
}

func limitOf(input Input) int {
	if input.Limit != nil && *input.Limit > 0 {
		return *input.Limit
	}
	return DefaultLimit
}

func contextOf(input Input) int {
	if input.Context != nil && *input.Context > 0 {
		return *input.Context
	}
	return 0
}

func runRipgrep(ctx context.Context, rg, root, base string, isDir bool, input Input) (string, error) {
	args := []string{"--color", "never", "--line-number"}
	if input.IgnoreCase {
		args = append(args, "-i")
	}
	if input.Literal {
		args = append(args, "-F")
	}
	if input.Glob != "" {
		args = append(args, "--glob", input.Glob)
	}
	if c := contextOf(input); c > 0 {
		args = append(args, "-C", strconv.Itoa(c))
	}

	var dir, pathArg string
	if isDir {
		dir = filepath.Join(root, filepath.FromSlash(base))
		pathArg = "."
	} else {
		dir = filepath.Join(root, filepath.FromSlash(path.Dir(base)))
		pathArg = path.Base(base)
	}
	args = append(args, input.Pattern, pathArg)

	cmd := exec.CommandContext(ctx, rg, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found", nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("invalid regex pattern: %s", msg)
	}

	return capLines(stdout.String(), limitOf(input)), nil
}

// capLines trims trailing newlines and caps the output at limit lines,
// appending a truncation note when lines were dropped.
func capLines(out string, limit int) string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return "No matches found"
	}
	lines := strings.Split(out, "\n")
	if len(lines) > limit {
		lines = lines[:limit]
		return strings.Join(lines, "\n") + fmt.Sprintf("\n... (results truncated to %d)", limit)
	}
	return strings.Join(lines, "\n")
}

func runPureGo(fsys fs.FS, base string, isDir bool, input Input) (string, error) {
	re, err := compilePattern(input)
	if err != nil {
		return "", err
	}

	files, err := filesToSearch(fsys, base, isDir, input.Glob)
	if err != nil {
		return "", err
	}

	limit := limitOf(input)
	ctxLines := contextOf(input)

	var out []string
	matches := 0
	truncated := false

	for _, file := range files {
		if matches >= limit {
			truncated = true
			break
		}
		lines, readErr := readLines(fsys, file)
		if readErr != nil {
			continue
		}
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			if matches >= limit {
				truncated = true
				break
			}
			start := i - ctxLines
			if start < 0 {
				start = 0
			}
			for j := start; j < i; j++ {
				out = append(out, fmt.Sprintf("%s:%d-%s", file, j+1, lines[j]))
			}
			out = append(out, fmt.Sprintf("%s:%d:%s", file, i+1, line))
			matches++
			end := i + ctxLines
			if end >= len(lines) {
				end = len(lines) - 1
			}
			for j := i + 1; j <= end; j++ {
				out = append(out, fmt.Sprintf("%s:%d-%s", file, j+1, lines[j]))
			}
		}
	}

	if len(out) == 0 {
		return "No matches found", nil
	}
	res := strings.Join(out, "\n")
	if truncated {
		res += fmt.Sprintf("\n... (results truncated to %d)", limit)
	}
	return res, nil
}

// compilePattern builds the search regexp, honoring literal and
// case-insensitive options.
func compilePattern(input Input) (*regexp.Regexp, error) {
	pattern := input.Pattern
	if input.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if input.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}
	return re, nil
}

func readLines(fsys fs.FS, name string) ([]string, error) {
	file, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func filesToSearch(fsys fs.FS, base string, isDir bool, globPattern string) ([]string, error) {
	if !isDir {
		return []string{base}, nil
	}

	if globPattern != "" {
		sub, err := fs.Sub(fsys, base)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}
		globMatches, err := doublestar.Glob(sub, globPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}

		var files []string
		for _, m := range globMatches {
			fi, statErr := fs.Stat(sub, m)
			if statErr == nil && !fi.IsDir() {
				files = append(files, path.Join(base, m))
			}
		}
		return files, nil
	}

	var files []string
	err := fs.WalkDir(fsys, base, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip hidden directories.
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
			return fs.SkipDir
		}

		// Skip hidden files.
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})

	return files, err
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
