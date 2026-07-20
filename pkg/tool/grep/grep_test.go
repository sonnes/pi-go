package grep

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/sandbox"
)

// noRootFS wraps a sandbox.FS but hides Root(), forcing grep onto its
// pure-Go search path instead of ripgrep.
type noRootFS struct{ fsys *sandbox.FS }

func (f noRootFS) Open(name string) (fs.File, error)   { return f.fsys.Open(name) }
func (f noRootFS) ResolveDir(p string) (string, error) { return f.fsys.ResolveDir(p) }

func setupTestFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"file1.go":      "package main\n\nfunc Hello() {\n\treturn\n}\n",
		"file2.go":      "package test\n\nfunc World() {\n\treturn\n}\n",
		"file3.txt":     "Hello World\nThis is a test\nHello again\n",
		"sub/nested.go": "package sub\n\nfunc Nested() {}\n",
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		err := os.MkdirAll(filepath.Dir(path), 0755)
		require.NoError(t, err)
		err = os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}

	return dir
}

func TestGrep_BasicMatch(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "Hello"})

	require.NoError(t, err)
	assert.Contains(t, result, "file3.txt")
}

func TestGrep_ContentFormat(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "Hello World"})

	require.NoError(t, err)
	// content mode: file:line:content
	assert.Contains(t, result, "file3.txt:1:Hello World")
}

func TestGrep_NoMatch(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "nonexistent_pattern_xyz"})

	require.NoError(t, err)
	assert.Equal(t, "No matches found", result)
}

func TestGrep_InvalidRegex(t *testing.T) {
	dir := setupTestFiles(t)
	// noRootFS has no Root(), forcing the pure-Go path which compiles the regex.
	run := New(FS{noRootFS{sandbox.New(dir, sandbox.Strict())}})

	_, err := run(context.Background(), Input{Pattern: "[invalid"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex")
}

func TestGrep_CaseInsensitive(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	// Without case insensitive: lowercase "hello" does not match "Hello".
	result1, err := run(context.Background(), Input{Pattern: "hello"})
	require.NoError(t, err)
	assert.Equal(t, "No matches found", result1)

	// With case insensitive.
	result2, err := run(context.Background(), Input{Pattern: "hello", IgnoreCase: true})
	require.NoError(t, err)
	assert.Contains(t, result2, "file3.txt")
}

func TestGrep_Literal(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "code.go"), []byte("x := a.b(c)\n"), 0644))
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "a.b(c)", Literal: true})

	require.NoError(t, err)
	assert.Contains(t, result, "code.go")
}

func TestGrep_GlobFilter(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "func", Glob: "*.go"})

	require.NoError(t, err)
	assert.Contains(t, result, "file1.go")
	assert.Contains(t, result, "file2.go")
	assert.NotContains(t, result, "file3.txt")
}

func TestGrep_RecursiveGlob(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "func", Glob: "**/*.go"})

	require.NoError(t, err)
	assert.Contains(t, result, "nested.go")
}

func TestGrep_Context(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "f.txt"),
		[]byte("before\nMATCH\nafter\n"),
		0644,
	))
	// Pure-Go path exercises the tool's own context handling.
	run := New(FS{noRootFS{sandbox.New(dir, sandbox.Strict())}})

	ctxLines := 1
	result, err := run(context.Background(), Input{Pattern: "MATCH", Context: &ctxLines})

	require.NoError(t, err)
	assert.Contains(t, result, "before")
	assert.Contains(t, result, "MATCH")
	assert.Contains(t, result, "after")
}

func TestGrep_PathNotExist(t *testing.T) {
	run := New(FS{sandbox.New("/nonexistent/path", sandbox.Strict())})

	_, err := run(context.Background(), Input{Pattern: "test"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestGrep_AbsolutePath_OutsideWorkingDir_Rejected(t *testing.T) {
	dir := setupTestFiles(t)
	otherDir := t.TempDir()
	run := New(FS{sandbox.New(otherDir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Pattern: "func", Path: dir})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes working directory")
}

func TestGrep_SpecificPath(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "func", Path: "sub"})

	require.NoError(t, err)
	assert.Contains(t, result, "nested.go")
	assert.NotContains(t, result, "file1.go")
}

func TestGrep_HiddenFilesSkipped(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("visible"), 0644)
	require.NoError(t, err)

	// Pure-Go path: skips hidden files.
	run := New(FS{noRootFS{sandbox.New(dir, sandbox.Strict())}})

	result, err := run(context.Background(), Input{Pattern: ".*"})

	require.NoError(t, err)
	assert.Contains(t, result, "visible.txt")
	assert.NotContains(t, result, ".hidden")
}

func TestGrep_Limit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "many.txt"),
		[]byte("hit\nhit\nhit\nhit\nhit\n"),
		0644,
	))
	run := New(FS{noRootFS{sandbox.New(dir, sandbox.Strict())}})

	limit := 2
	result, err := run(context.Background(), Input{Pattern: "hit", Limit: &limit})

	require.NoError(t, err)
	assert.Contains(t, result, "truncated to 2")
}

func TestGrep_FallsBackToPureGoWithoutRoot(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{noRootFS{sandbox.New(dir, sandbox.Strict())}})

	result, err := run(context.Background(), Input{Pattern: "Hello"})

	require.NoError(t, err)
	assert.Contains(t, result, "file3.txt")
}
