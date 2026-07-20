package edit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/sandbox"
)

func TestEdit_SingleReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("hello world"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{
		Path:  path,
		Edits: []Edit{{OldString: "world", NewString: "universe"}},
	})

	require.NoError(t, err)
	assert.Contains(t, result, "Successfully applied 1 edit")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello universe", string(content))
}

func TestEdit_FlatReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("hello world"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{
		Path:      path,
		OldString: "world",
		NewString: "universe",
	})

	require.NoError(t, err)
	assert.Contains(t, result, "Successfully applied 1 edit")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello universe", string(content))
}

func TestEdit_FlatAndBatchTogetherRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{
		Path:      path,
		OldString: "hello",
		NewString: "hi",
		Edits:     []Edit{{OldString: "world", NewString: "there"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not both")
}

func TestEdit_MultipleDisjointEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("alpha beta gamma"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{
		Path: path,
		Edits: []Edit{
			{OldString: "alpha", NewString: "A"},
			{OldString: "gamma", NewString: "G"},
		},
	})

	require.NoError(t, err)
	assert.Contains(t, result, "Successfully applied 2 edit")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "A beta G", string(content))
}

func TestEdit_NotUnique(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("hello hello"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{
		Path:  path,
		Edits: []Edit{{OldString: "hello", NewString: "hi"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "appears 2 times")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello hello", string(content))
}

func TestEdit_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("hello world"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{
		Path:  path,
		Edits: []Edit{{OldString: "nonexistent", NewString: "replacement"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestEdit_Overlapping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("abcdef"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{
		Path: path,
		Edits: []Edit{
			{OldString: "abc", NewString: "X"},
			{OldString: "cde", NewString: "Y"},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "overlap")
}

func TestEdit_NoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("hello world"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{
		Path:  path,
		Edits: []Edit{{OldString: "hello", NewString: "hello"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no changes made")
}

func TestEdit_EmptyOldText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("hello world"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{
		Path:  path,
		Edits: []Edit{{OldString: "", NewString: "test"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestEdit_EmptyEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("hello world"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{
		Path:  path,
		Edits: []Edit{},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "edits cannot be empty")
}

func TestEdit_PreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("line1\r\nline2\r\n"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{
		Path:  path,
		Edits: []Edit{{OldString: "line2", NewString: "LINE2"}},
	})

	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "line1\r\nLINE2\r\n", string(content))
}

func TestEdit_RelativePath_ResolvedAgainstWorkingDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := os.WriteFile(path, []byte("hello world"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err = run(context.Background(), Input{
		Path:  "test.txt",
		Edits: []Edit{{OldString: "world", NewString: "universe"}},
	})

	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello universe", string(content))
}

func TestEdit_RelativePath_EscapesWorkingDir(t *testing.T) {
	dir := t.TempDir()
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{
		Path:  "../outside.txt",
		Edits: []Edit{{OldString: "a", NewString: "b"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes working directory")
}

func TestEdit_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{
		Path:  filepath.Join(dir, "missing.txt"),
		Edits: []Edit{{OldString: "a", NewString: "b"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read")
}
