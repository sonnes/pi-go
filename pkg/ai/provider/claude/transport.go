package claude

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// sendArgs holds parameters for a single subprocess invocation.
type sendArgs struct {
	prompt        string
	systemPrompt  string
	jsonSchema    string
	noPersistence bool
}

// spawn starts a one-shot `claude --print` subprocess and returns its
// stdout pipe plus a cleanup func that waits for the process to exit.
// It holds no state and is safe to call concurrently.
func spawn(
	ctx context.Context,
	cfg config,
	args sendArgs,
) (io.ReadCloser, func() error, error) {
	cliArgs := buildArgs(cfg, args)
	cmd := exec.Command(cfg.cliPath, cliArgs...)

	if cfg.workDir != "" {
		cmd.Dir = cfg.workDir
	}
	cmd.Env = cleanEnv()
	if len(cfg.env) > 0 {
		cmd.Env = append(cmd.Env, cfg.env...)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("claude: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("claude: start: %w", err)
	}

	var (
		waitOnce sync.Once
		waitErr  error
		waitCh   = make(chan struct{})
	)
	doWait := func() {
		waitOnce.Do(func() {
			waitErr = cmd.Wait()
			close(waitCh)
		})
	}

	cleanup := func() error {
		doWait()
		<-waitCh
		if waitErr != nil && stderr.Len() > 0 {
			return fmt.Errorf("claude: %w: %s", waitErr, stderr.String())
		}
		return waitErr
	}

	go func() {
		select {
		case <-ctx.Done():
			gracefulShutdown(cmd, waitCh)
			doWait()
		case <-waitCh:
		}
	}()

	return stdout, cleanup, nil
}

// buildArgs constructs CLI arguments. The provider always runs the CLI
// with --no-session-persistence so each call is non-persisted.
func buildArgs(cfg config, args sendArgs) []string {
	a := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
	}

	if args.noPersistence {
		a = append(a, "--no-session-persistence")
	}
	if cfg.model != "" {
		a = append(a, "--model", cfg.model)
	}
	if len(cfg.allowedTools) > 0 {
		a = append(a, "--allowedTools", strings.Join(cfg.allowedTools, ","))
	}
	if cfg.maxTurns > 0 {
		a = append(a, "--max-turns", strconv.Itoa(cfg.maxTurns))
	}
	for _, dir := range cfg.addDirs {
		a = append(a, "--add-dir", dir)
	}
	if args.systemPrompt != "" {
		a = append(a, "--system-prompt", args.systemPrompt)
	}
	if args.jsonSchema != "" {
		a = append(a, "--json-schema", args.jsonSchema)
	}
	if args.prompt != "" {
		a = append(a, args.prompt)
	}

	return a
}

// cleanEnv returns the current environment minus variables that would
// prevent a fresh Claude Code launch (e.g. nested-session detection).
func cleanEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			continue
		}
		env = append(env, e)
	}
	return env
}

// gracefulShutdown sends SIGINT, waits 3 seconds, then SIGKILL.
func gracefulShutdown(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd.Process == nil {
		return
	}

	_ = cmd.Process.Signal(syscall.SIGINT)

	select {
	case <-done:
		return
	case <-time.After(3 * time.Second):
	}

	_ = cmd.Process.Kill()
}
