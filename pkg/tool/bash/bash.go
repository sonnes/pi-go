// Package bash provides the Bash tool. The tool runs shell commands.
package bash

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "embed"
)

// Description is the default tool documentation, embedded from
// description.md. A client registers the tool under any name. The client
// can pass this text, or its own text, as the description.
//
//go:embed description.md
var Description string

// ToolName is the default name for this tool in the registry.
const ToolName = "bash"

// MaxOutputLength is the maximum output length before truncation.
const MaxOutputLength = 100000

// Input defines the parameters for the Bash tool.
type Input struct {
	Timeout *int   `json:"timeout,omitempty" jsonschema:"Optional timeout in seconds. Omit to run without a timeout."`
	Command string `json:"command" jsonschema:"The shell command to run."`
}

// bash runs shell commands. The working directory stays alive across
// calls, so a `cd` in one command carries into the next. This type holds
// only the tool's real dependencies. Identity (name, description) belongs
// to the client that registers the tool.
type bash struct {
	shell string
	cwd   string
	mu    sync.Mutex
}

// Option configures a Bash tool.
type Option interface{ apply(*bash) }

// Dir sets the initial working directory that the command runs in.
type Dir string

func (d Dir) apply(b *bash) { b.cwd = string(d) }

// Shell overrides the shell that runs commands. The default is "bash".
type Shell string

func (s Shell) apply(b *bash) { b.shell = string(s) }

// New returns the Bash tool's runner. The runner runs one shell command
// and returns the combined stdout and stderr. If the output is very long,
// the runner truncates it. The working directory persists across calls on
// the same runner. Pass the runner to [ai.DefineTool] with a name and
// description.
func New(opts ...Option) func(context.Context, Input) (string, error) {
	b := &bash{shell: "bash"}
	for _, o := range opts {
		o.apply(b)
	}
	return b.run
}

func (b *bash) run(ctx context.Context, input Input) (string, error) {
	if input.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	b.mu.Lock()
	dir := b.cwd
	b.mu.Unlock()

	// If the caller gives a timeout in seconds, the tool applies it.
	var timeout time.Duration
	if input.Timeout != nil && *input.Timeout > 0 {
		timeout = time.Duration(*input.Timeout) * time.Second
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	sentinel := fmt.Sprintf("__PIGO_CWD_%d__", time.Now().UnixNano())
	command := commandWithCwdSentinel(input.Command, sentinel)
	cmd := exec.CommandContext(ctx, b.shell, shellCommandArgs(b.shell, command)...)
	cmd.Env = os.Environ()
	cmd.Dir = dir

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command
	err := cmd.Run()

	stdoutText, cwd, ok := stripCwdSentinel(stdout.String(), sentinel)
	if ok {
		b.setCwd(cwd)
	}

	// Combine output
	output := stdoutText
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	// Truncate output longer than MaxOutputLength
	if len(output) > MaxOutputLength {
		half := MaxOutputLength / 2
		output = output[:half] + "\n\n... (output truncated) ...\n\n" + output[len(output)-half:]
	}

	// Handle errors
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return output, fmt.Errorf("command timed out after %v", timeout)
		}
		if output != "" {
			output += "\n"
		}
		output += fmt.Sprintf("Exit error: %v", err)
	}

	if output == "" {
		return "(no output)", nil
	}

	return output, nil
}

func (b *bash) setCwd(cwd string) {
	if cwd == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cwd = filepath.Clean(cwd)
}

func commandWithCwdSentinel(command, sentinel string) string {
	return fmt.Sprintf(
		"%s\n__pigo_status=$?\nprintf '\\n%s%%s\\n' \"$PWD\"\nexit $__pigo_status",
		command,
		sentinel,
	)
}

func stripCwdSentinel(stdout, sentinel string) (string, string, bool) {
	idx := strings.LastIndex(stdout, sentinel)
	if idx < 0 {
		return stdout, "", false
	}

	before := strings.TrimSuffix(stdout[:idx], "\n")
	after := stdout[idx+len(sentinel):]
	cwd, rest, _ := strings.Cut(after, "\n")
	cwd = strings.TrimSpace(cwd)
	rest = strings.TrimLeft(rest, "\n")
	if rest != "" {
		if before != "" {
			before += "\n"
		}
		before += rest
	}
	return before, cwd, true
}

func shellCommandArgs(shell, command string) []string {
	switch filepath.Base(shell) {
	case "bash", "zsh":
		return []string{"-lc", command}
	default:
		return []string{"-c", command}
	}
}
