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

// sendArgs holds parameters for a single send call.
type sendArgs struct {
	prompt    string
	sessionID string
	resume    bool
}

// send spawns a new claude process and returns a reader for NDJSON
// response lines. The caller must call the returned cleanup func
// when done reading.
func (a *Agent) send(
	ctx context.Context,
	args sendArgs,
) (io.ReadCloser, func() error, error) {
	cliArgs := buildArgs(a.cfg, args)
	cmd := exec.Command(a.cfg.cliPath, cliArgs...)

	if a.cfg.workDir != "" {
		cmd.Dir = a.cfg.workDir
	}
	cmd.Env = cleanEnv()
	if len(a.cfg.env) > 0 {
		cmd.Env = append(cmd.Env, a.cfg.env...)
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

	// cmd.Wait must be called exactly once. Use sync.Once to share
	// the result between the cleanup func and the cancellation goroutine.
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

	// Handle context cancellation with graceful shutdown.
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

// buildArgs constructs CLI arguments for a spawn-per-call invocation.
func buildArgs(cfg config, args sendArgs) []string {
	a := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
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
	if args.resume && args.sessionID != "" {
		a = append(a, "--resume", args.sessionID)
	}
	if args.prompt != "" {
		a = append(a, args.prompt)
	}

	return a
}

// cleanEnv returns the current environment with variables removed that
// would prevent Claude Code from launching (e.g. nested session detection).
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
// The done channel must close when the process exits.
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
