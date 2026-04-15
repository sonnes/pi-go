package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// transportIface is the interface consumed by [Agent] for a single CLI
// subprocess. Implemented by the real [*transport] and by test fakes.
type transportIface interface {
	// writeUserMessage writes a single SDKUserMessage NDJSON line to stdin.
	writeUserMessage(line []byte) error
	// stdout returns the reader for subprocess stdout.
	stdout() io.Reader
	// exited returns a channel that closes after the subprocess terminates.
	exited() <-chan struct{}
	// exitErr returns the subprocess error once [exited] has closed.
	exitErr() error
	// close terminates the subprocess and returns any exit error.
	close() error
}

// transport owns the long-lived `claude --print --input-format stream-json`
// subprocess. A single transport serves many turns: each [Agent.Send] writes
// one SDKUserMessage line to stdin and the Agent's reader goroutine
// demultiplexes the NDJSON output.
type transport struct {
	cmd       *exec.Cmd
	stdinPipe io.WriteCloser
	stdoutR   io.ReadCloser
	stderrBuf *bytes.Buffer
	writeMu   sync.Mutex

	// exitedCh is closed by [transport.waitLoop] after the subprocess terminates.
	exitedCh chan struct{}
	// exitErrVal holds the subprocess Wait error.
	exitErrVal error
	exitedOnce sync.Once
}

// newTransport starts the Claude CLI subprocess and begins waiting on it.
// The caller owns reading from [transport.stdout] and must call
// [transport.close] when done.
func newTransport(ctx context.Context, cfg config) (transportIface, error) {
	cliArgs := buildArgs(cfg)
	cmd := exec.Command(cfg.cliPath, cliArgs...)

	if cfg.workDir != "" {
		cmd.Dir = cfg.workDir
	}
	cmd.Env = cleanEnv()
	if len(cfg.env) > 0 {
		cmd.Env = append(cmd.Env, cfg.env...)
	}

	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude: stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude: stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude: start: %w", err)
	}

	t := &transport{
		cmd:       cmd,
		stdinPipe: stdin,
		stdoutR:   stdout,
		stderrBuf: stderr,
		exitedCh:  make(chan struct{}),
	}

	go t.waitLoop()

	// If the parent context is cancelled, tear down the subprocess.
	go func() {
		select {
		case <-ctx.Done():
			t.shutdown()
		case <-t.exitedCh:
		}
	}()

	return t, nil
}

// Ensure *transport implements transportIface at compile time.
var _ transportIface = (*transport)(nil)

// stdout returns the reader for subprocess stdout.
func (t *transport) stdout() io.Reader { return t.stdoutR }

// exited returns a channel that closes after the subprocess terminates.
func (t *transport) exited() <-chan struct{} { return t.exitedCh }

// exitErr returns the subprocess error once [exited] has closed.
func (t *transport) exitErr() error { return t.exitErrVal }

// waitLoop blocks on cmd.Wait and records the result.
func (t *transport) waitLoop() {
	err := t.cmd.Wait()
	t.exitedOnce.Do(func() {
		if err != nil && t.stderrBuf.Len() > 0 {
			t.exitErrVal = fmt.Errorf("claude: %w: %s", err, t.stderrBuf.String())
		} else if err != nil {
			t.exitErrVal = fmt.Errorf("claude: %w", err)
		}
		close(t.exitedCh)
	})
}

// writeUserMessage writes a single SDKUserMessage NDJSON line to stdin.
// The write is serialized so concurrent Sends cannot interleave frames.
func (t *transport) writeUserMessage(line []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	select {
	case <-t.exitedCh:
		if t.exitErrVal != nil {
			return t.exitErrVal
		}
		return errors.New("claude: subprocess has exited")
	default:
	}

	_, err := t.stdinPipe.Write(line)
	return err
}

// close shuts down the subprocess. Closing stdin gives the CLI a chance to
// drain; if it doesn't exit within 3 s, [gracefulShutdown] escalates to
// SIGINT then SIGKILL.
func (t *transport) close() error {
	t.shutdown()
	<-t.exitedCh
	return t.exitErrVal
}

// shutdown closes stdin and, if the process hasn't exited after a grace
// period, signals it.
func (t *transport) shutdown() {
	_ = t.stdinPipe.Close()

	select {
	case <-t.exitedCh:
		return
	case <-time.After(3 * time.Second):
	}

	gracefulShutdown(t.cmd, t.exitedCh)
}

// buildArgs constructs CLI arguments for a persistent stream-json subprocess.
// The positional prompt argument is never used — prompts arrive on stdin.
func buildArgs(cfg config) []string {
	a := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
	}

	if cfg.model != "" {
		a = append(a, "--model", cfg.model)
	}
	if len(cfg.allowedTools) > 0 {
		a = append(a, "--allowedTools", strings.Join(cfg.allowedTools, ","))
	}
	if cfg.toolsSet || len(cfg.tools) > 0 {
		a = append(a, "--tools", strings.Join(cfg.tools, ","))
	}
	if len(cfg.disallowedTools) > 0 {
		a = append(a, "--disallowedTools", strings.Join(cfg.disallowedTools, ","))
	}
	if cfg.maxTurns > 0 {
		a = append(a, "--max-turns", strconv.Itoa(cfg.maxTurns))
	}
	for _, dir := range cfg.addDirs {
		a = append(a, "--add-dir", dir)
	}
	if cfg.agent != "" {
		a = append(a, "--agent", cfg.agent)
	}
	if len(cfg.agents) > 0 {
		if b, err := json.Marshal(cfg.agents); err == nil {
			a = append(a, "--agents", string(b))
		}
	}
	if cfg.systemPrompt != "" {
		a = append(a, "--system-prompt", cfg.systemPrompt)
	}
	if cfg.appendSystemPrompt != "" {
		a = append(a, "--append-system-prompt", cfg.appendSystemPrompt)
	}
	if cfg.sessionID != "" {
		a = append(a, "--resume", cfg.sessionID)
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
