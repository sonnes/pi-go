package bash

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBash_SimpleCommand(t *testing.T) {
	run := New()

	result, err := run(context.Background(), Input{Command: "echo hello"})

	require.NoError(t, err)
	assert.Equal(t, "hello\n", result)
}

func TestBash_CommandWithStderr(t *testing.T) {
	run := New()

	result, err := run(context.Background(), Input{Command: "echo stdout; echo stderr >&2"})

	require.NoError(t, err)
	assert.Contains(t, result, "stdout")
	assert.Contains(t, result, "stderr")
}

func TestBash_EmptyCommand(t *testing.T) {
	run := New()

	_, err := run(context.Background(), Input{Command: ""})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestBash_Timeout(t *testing.T) {
	run := New()

	timeout := 1
	_, err := run(context.Background(), Input{Command: "sleep 10", Timeout: &timeout})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestBash_ExitError(t *testing.T) {
	run := New()

	result, err := run(context.Background(), Input{Command: "exit 1"})

	require.NoError(t, err)
	assert.Contains(t, result, "Exit error")
}

func TestBash_WorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	run := New(Dir(tmpDir))

	result, err := run(context.Background(), Input{Command: "pwd"})

	require.NoError(t, err)
	assert.Contains(t, result, tmpDir)
}

func TestBash_NoOutput(t *testing.T) {
	run := New()

	result, err := run(context.Background(), Input{Command: "true"})

	require.NoError(t, err)
	assert.Equal(t, "(no output)", result)
}

func TestBash_OutputTruncation(t *testing.T) {
	run := New()

	result, err := run(context.Background(), Input{Command: "yes | head -n 100000"})

	require.NoError(t, err)
	assert.Contains(t, result, "truncated")
	assert.LessOrEqual(t, len(result), MaxOutputLength+100)
}

func TestBash_CustomShell(t *testing.T) {
	run := New(Shell("sh"))

	result, err := run(context.Background(), Input{Command: "echo $0"})

	require.NoError(t, err)
	assert.Contains(t, result, "sh")
}

func TestBash_DescriptionMatchesAdvertisedCapabilities(t *testing.T) {
	desc := strings.ToLower(Description)

	assert.Contains(t, desc, "working directory persists")
	assert.Contains(t, desc, "initialized from the user's profile")
}

func TestBash_PipedCommands(t *testing.T) {
	run := New()

	result, err := run(context.Background(), Input{Command: "echo hello world | wc -w"})

	require.NoError(t, err)
	assert.Contains(t, strings.TrimSpace(result), "2")
}

func TestBash_DefaultValues(t *testing.T) {
	start := time.Now()
	run := New()

	_, err := run(context.Background(), Input{Command: "echo fast"})

	require.NoError(t, err)
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 5*time.Second)
}

func TestBash_WorkingDirectoryPersistsBetweenCommands(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(subdir, 0o755))

	run := New(Dir(dir))

	result, err := run(context.Background(), Input{Command: "cd sub"})
	require.NoError(t, err)

	result, err = run(context.Background(), Input{Command: "pwd"})
	require.NoError(t, err)
	want := subdir
	if resolved, resolveErr := filepath.EvalSymlinks(subdir); resolveErr == nil {
		want = resolved
	}
	assert.Equal(t, want, strings.TrimSpace(result))
}

func TestBash_InitializesBashFromUserProfile(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".bash_profile"),
		[]byte("export PIGO_PROFILE_MARKER=loaded\n"),
		0o644,
	))

	run := New(Dir(t.TempDir()), Shell("bash"))
	result, err := run(context.Background(), Input{Command: `printf %s "$PIGO_PROFILE_MARKER"`})
	require.NoError(t, err)
	assert.Equal(t, "loaded", result)
}
