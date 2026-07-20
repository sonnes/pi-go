package read

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/sandbox"
)

func TestRead_ReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "line 1\nline 2\nline 3\n"
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Path: path})

	require.NoError(t, err)
	assert.Contains(t, result.Content, "line 1")
	assert.Contains(t, result.Content, "line 2")
	assert.Contains(t, result.Content, "line 3")
}

func TestRead_RelativePath_ResolvedAgainstWorkingDir(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello\n"), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Path: "test.txt"})

	require.NoError(t, err)
	assert.Contains(t, result.Content, "hello")
}

func TestRead_RelativePath_EscapesWorkingDir(t *testing.T) {
	dir := t.TempDir()
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: "../outside.txt"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes working directory")
}

func TestRead_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: filepath.Join(dir, "missing.txt")})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open")
}

func TestRead_Directory(t *testing.T) {
	dir := t.TempDir()
	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	_, err := run(context.Background(), Input{Path: "."})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot read directory")
}

func TestRead_Offset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	offset := 3
	result, err := run(context.Background(), Input{Path: path, Offset: &offset})

	require.NoError(t, err)
	assert.NotContains(t, result.Content, "line 1")
	assert.NotContains(t, result.Content, "line 2")
	assert.Contains(t, result.Content, "line 3")
	assert.Contains(t, result.Content, "line 4")
}

func TestRead_Limit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	limit := 2
	result, err := run(context.Background(), Input{Path: path, Limit: &limit})

	require.NoError(t, err)
	assert.Contains(t, result.Content, "line 1")
	assert.Contains(t, result.Content, "line 2")
	assert.NotContains(t, result.Content, "line 3")
}

func TestRead_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	err := os.WriteFile(path, []byte{}, 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Path: path})

	require.NoError(t, err)
	assert.Equal(t, "(empty file)", result.Content)
}

func TestRead_LineNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "first\nsecond\nthird\n"
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Path: path})

	require.NoError(t, err)
	assert.Contains(t, result.Content, "1\t")
	assert.Contains(t, result.Content, "2\t")
	assert.Contains(t, result.Content, "3\t")
}

func TestRead_DefaultLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	var content strings.Builder
	for i := 1; i <= 2500; i++ {
		content.WriteString("line\n")
	}
	err := os.WriteFile(path, []byte(content.String()), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	result, err := run(context.Background(), Input{Path: path})

	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	assert.Equal(t, DefaultLimit, len(lines))
}

func TestRead_ImageFileReturnsImageResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pixel.png")
	data, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=",
	)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))

	run := New(FS{sandbox.New(dir, sandbox.Strict())})
	result, err := run(context.Background(), Input{Path: path})

	require.NoError(t, err)
	assert.Equal(t, "image", result.Type)
	assert.Equal(t, "image/png", result.MediaType)
	assert.Equal(t, data, result.Data)
}

func TestRead_NotebookRendersCellsAndOutputs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notebook.ipynb")
	content := `{
  "cells": [
    {"cell_type": "markdown", "source": ["# Title\n", "Intro"]},
    {"cell_type": "code", "source": ["print(\"hi\")"], "outputs": [
      {"output_type": "stream", "name": "stdout", "text": ["hi\n"]}
    ]}
  ]
}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	run := New(FS{sandbox.New(dir, sandbox.Strict())})
	result, err := run(context.Background(), Input{Path: path})

	require.NoError(t, err)
	assert.Contains(t, result.Content, "Cell 1 [markdown]")
	assert.Contains(t, result.Content, "# Title")
	assert.Contains(t, result.Content, "Cell 2 [code]")
	assert.Contains(t, result.Content, "stdout")
	assert.Contains(t, result.Content, "hi")
}

func TestRead_PDFRendersExtractedText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.pdf")
	content := "%PDF-1.1\n1 0 obj\n<<>>\nstream\nBT (Hello PDF) Tj ET\nendstream\nendobj\n%%EOF"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	run := New(FS{sandbox.New(dir, sandbox.Strict())})
	result, err := run(context.Background(), Input{Path: path})

	require.NoError(t, err)
	assert.Equal(t, "media", result.Type)
	assert.Equal(t, "application/pdf", result.MediaType)
	assert.Equal(t, []byte(content), result.Data)
	assert.Contains(t, result.Content, "Hello PDF")
}

func TestRead_OffsetBeyondFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "line 1\nline 2\nline 3\n"
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)

	run := New(FS{sandbox.New(dir, sandbox.Strict())})

	offset := 100
	result, err := run(context.Background(), Input{Path: path, Offset: &offset})

	require.NoError(t, err)
	assert.Equal(t, "(empty file)", result.Content)
}
