package claudecli

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

// sendArgs holds the parameters for one subprocess run.
type sendArgs struct {
	prompt          string
	systemPrompt    string
	jsonSchema      string
	effort          string
	noPersistence   bool
	resumeSession   string
	partialMessages bool
}

// spawn starts a one-shot `claude --print` subprocess. It returns the
// stdout pipe of the subprocess and a cleanup func. The cleanup func
// waits for the process to exit. spawn holds no state, and concurrent
// calls are safe.
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

// buildArgs builds the CLI arguments. If the caller sets no session ID,
// the provider adds --no-session-persistence, and the call persists no
// session.
func buildArgs(cfg config, args sendArgs) []string {
	a := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
	}

	if cfg.partialMessages || args.partialMessages {
		a = append(a, "--include-partial-messages")
	}
	if args.noPersistence {
		a = append(a, "--no-session-persistence")
	}
	if cfg.model != "" {
		a = append(a, "--model", cfg.model)
	}
	if args.effort != "" {
		a = append(a, "--effort", args.effort)
	}
	if len(cfg.allowedTools) > 0 {
		a = append(a, "--allowedTools", strings.Join(cfg.allowedTools, ","))
	}
	if cfg.maxTurns > 0 {
		a = append(a, "--max-turns", strconv.Itoa(cfg.maxTurns))
	}
	if cfg.maxBudgetUSD > 0 {
		a = append(
			a,
			"--max-budget-usd",
			strconv.FormatFloat(cfg.maxBudgetUSD, 'f', -1, 64),
		)
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
	if args.resumeSession != "" {
		a = append(a, "--resume", args.resumeSession)
	}
	if args.prompt != "" {
		a = append(a, args.prompt)
	}

	return a
}

// cleanEnv returns the current environment without the variables that
// prevent a new Claude Code start. Nested-session detection is one
// example.
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

// gracefulShutdown sends SIGINT. If the process does not exit in 3
// seconds, gracefulShutdown sends SIGKILL.
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
