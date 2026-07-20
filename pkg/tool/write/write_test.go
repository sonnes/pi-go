package write

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/sandbox"
)

func TestWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Path: path, Content: "hello world"})

	require.NoError(t, err)
	assert.Contains(t, result, "Successfully wrote")
	assert.Contains(t, result, "11 bytes")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestWrite_OverwriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	err := os.WriteFile(path, []byte("old content"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{Path: path, Content: "new content"})
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new content", string(content))
}

func TestWrite_CreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "path", "file.txt")

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: path, Content: "content"})
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
}

func TestWrite_RelativePath_ResolvedAgainstWorkingDir(t *testing.T) {
	dir := t.TempDir()

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: "sub/foo.txt", Content: "hello"})
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "sub", "foo.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func TestWrite_RelativePath_EscapesWorkingDir(t *testing.T) {
	dir := t.TempDir()

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: "../outside.txt", Content: "nope"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes working directory")
}

func TestWrite_AbsolutePath_OutsideWorkingDir_Rejected(t *testing.T) {
	dir := t.TempDir()
	otherDir := t.TempDir()

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: filepath.Join(otherDir, "sneaky.txt"), Content: "nope"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes working directory")
}

func TestWrite_ReturnsBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Path: path, Content: "12345"})

	require.NoError(t, err)
	assert.Contains(t, result, "5 bytes")
}

func TestWrite_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Path: path, Content: ""})

	require.NoError(t, err)
	assert.Contains(t, result, "0 bytes")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "", string(content))
}

func TestWrite_UnicodeContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unicode.txt")

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: path, Content: "日本語テスト"})
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "日本語テスト", string(content))
}

func TestWrite_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perms.txt")

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: path, Content: "test"})
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm())
}

func TestWrite_DirPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "newdir", "file.txt")

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: path, Content: "test"})
	require.NoError(t, err)

	info, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestWrite_LargeContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")

	content := strings.Repeat("a", 10000)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: path, Content: content})
	require.NoError(t, err)

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, len(content), len(written))
}

func TestWrite_ExistingDirAsFile(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	err := os.Mkdir(subdir, 0755)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{Path: subdir, Content: "test"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write")
}
