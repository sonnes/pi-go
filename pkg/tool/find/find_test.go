package find

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/sandbox"
)

func setupTestFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := []struct {
		path    string
		content string
	}{
		{"file1.go", "package main"},
		{"file2.go", "package test"},
		{"file3.txt", "hello"},
		{"sub/nested.go", "package sub"},
		{"sub/deep/file.go", "package deep"},
	}

	for _, f := range files {
		path := filepath.Join(dir, f.path)
		err := os.MkdirAll(filepath.Dir(path), 0755)
		require.NoError(t, err)
		err = os.WriteFile(path, []byte(f.content), 0644)
		require.NoError(t, err)
	}

	return dir
}

func TestFind_SimplePattern(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "*.go"})

	require.NoError(t, err)
	assert.Contains(t, result, "file1.go")
	assert.Contains(t, result, "file2.go")
	assert.NotContains(t, result, "file3.txt")
	assert.NotContains(t, result, "nested.go")
}

func TestFind_RecursivePattern(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "**/*.go"})

	require.NoError(t, err)
	assert.Contains(t, result, "file1.go")
	assert.Contains(t, result, "file2.go")
	assert.Contains(t, result, "nested.go")
	assert.Contains(t, result, "deep/file.go")
	assert.NotContains(t, result, "file3.txt")
}

func TestFind_NoMatch(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "*.xyz"})

	require.NoError(t, err)
	assert.Equal(t, "No files found", result)
}

func TestFind_InvalidPath(t *testing.T) {
	run := New(FS{sandbox.New("/nonexistent/path", sandbox.Strict())})

	_, err := run(context.Background(), Input{Pattern: "*.go"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestFind_PathIsFile(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Pattern: "*.go", Path: "file1.go"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestFind_Limit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.go", i)), []byte("x"), 0644)
		require.NoError(t, err)
	}
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	limit := 2
	result, err := run(context.Background(), Input{Pattern: "*.go", Limit: &limit})

	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(result), "\n")
	require.Len(t, lines, 3) // 2 results + truncation note
	assert.Contains(t, result, "truncated to 2")
}

func TestFind_CustomPath(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "*.go", Path: "sub"})

	require.NoError(t, err)
	assert.Contains(t, result, "nested.go")
	assert.NotContains(t, result, "file1.go")
}

func TestFind_AbsolutePathInsideWorkingDir(t *testing.T) {
	dir := setupTestFiles(t)
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "*.go", Path: filepath.Join(dir, "sub")})

	require.NoError(t, err)
	assert.Contains(t, result, "nested.go")
	assert.NotContains(t, result, "file1.go")
}

func TestFind_AbsolutePath_OutsideWorkingDir_Rejected(t *testing.T) {
	dir := setupTestFiles(t)
	otherDir := t.TempDir()
	run := New(FS{sandbox.New(otherDir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Pattern: "*.go", Path: dir})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes working directory")
}

func TestFind_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Pattern: "*"})

	require.NoError(t, err)
	assert.Equal(t, "No files found", result)
}
