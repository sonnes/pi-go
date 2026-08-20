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
	"sync/atomic"
	"syscall"
	"time"
)

// transportIface is the interface that [Agent] uses for one CLI
// subprocess. The real [*transport] and the test fakes implement it.
type transportIface interface {
	// writeUserMessage writes a single SDKUserMessage NDJSON line to stdin.
	writeUserMessage(line []byte) error
	// writeControlResponse writes a control_response NDJSON line to
	// stdin. The line answers a control_request from the CLI, for
	// example can_use_tool.
	writeControlResponse(line []byte) error
	// interrupt writes a stream-json control_request line to stdin. The
	// line asks the CLI to abort the turn in progress. The subprocess
	// keeps running.
	interrupt() error
	// stdout returns the reader for subprocess stdout.
	stdout() io.Reader
	// exited returns a channel that closes after the subprocess exits.
	exited() <-chan struct{}
	// exitErr returns the subprocess error once [exited] has closed.
	exitErr() error
	// close stops the subprocess and returns any exit error.
	close() error
}

// transport owns the long-lived
// `claude --print --input-format stream-json` subprocess. One transport
// serves many turns. Each [Agent.Send] writes one SDKUserMessage line to
// stdin. The reader goroutine of the Agent then demultiplexes the NDJSON
// output.
type transport struct {
	cmd       *exec.Cmd
	stdinPipe io.WriteCloser
	stdoutR   io.ReadCloser
	stderrBuf *bytes.Buffer
	writeMu   sync.Mutex
	// interruptSeq generates monotonic control_request IDs for interrupt().
	interruptSeq atomic.Uint64

	// exitedCh closes after the subprocess exits. [transport.waitLoop]
	// closes this channel.
	exitedCh chan struct{}
	// exitErrVal holds the subprocess Wait error.
	exitErrVal error
	exitedOnce sync.Once
}

// newTransport starts the Claude CLI subprocess and waits on it. The
// caller reads from [transport.stdout]. The caller must call
// [transport.close] when it is done.
//
// The subprocess is persistent and serves many turns. [transport.close],
// which [Agent.Close] calls, owns the lifetime of the subprocess. The
// context of one turn does NOT own it. [transport.interrupt] aborts a
// turn and leaves the subprocess running for the next turn. newTransport
// accepts ctx only to match the signature of the injectable factory. The
// ctx does not control the shutdown of the subprocess.
func newTransport(_ context.Context, cfg config) (transportIface, error) {
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

	return t, nil
}

// Make sure that *transport implements transportIface at compile time.
var _ transportIface = (*transport)(nil)

// stdout returns the reader for subprocess stdout.
func (t *transport) stdout() io.Reader { return t.stdoutR }

// exited returns a channel that closes after the subprocess exits.
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

// writeUserMessage writes one SDKUserMessage NDJSON line to stdin. The
// writes are serialized, so concurrent Sends cannot interleave frames.
func (t *transport) writeUserMessage(line []byte) error {
	return t.writeLine(line)
}

// writeControlResponse writes a control_response NDJSON line to stdin.
func (t *transport) writeControlResponse(line []byte) error {
	return t.writeLine(line)
}

// writeLine writes one NDJSON line to stdin. The writes are serialized,
// so concurrent writers cannot interleave frames.
func (t *transport) writeLine(line []byte) error {
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

// interrupt writes a stream-json control_request. The request asks the
// CLI to abort the turn in progress. The subprocess keeps running, so
// the next [Agent.Send] reuses the same session. The CLI ends the
// current turn with a result line. The reader converts that line into
// the [agent.EventAgentEnd] of the turn.
func (t *transport) interrupt() error {
	id := "req_" + strconv.FormatUint(t.interruptSeq.Add(1), 10)
	line, err := buildInterruptControl(id)
	if err != nil {
		return err
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	select {
	case <-t.exitedCh:
		// The subprocess already exited. There is nothing to interrupt.
		return nil
	default:
	}

	_, err = t.stdinPipe.Write(line)
	return err
}

// controlRequest is the stream-json control envelope that the Claude CLI
// accepts on stdin. It matches the interrupt request of the Agent SDK.
type controlRequest struct {
	Type      string             `json:"type"`
	RequestID string             `json:"request_id"`
	Request   controlRequestBody `json:"request"`
}

type controlRequestBody struct {
	Subtype string `json:"subtype"`
}

// buildInterruptControl encodes one interrupt control_request line for
// the given request ID. The line ends with a newline.
func buildInterruptControl(requestID string) ([]byte, error) {
	b, err := json.Marshal(controlRequest{
		Type:      "control_request",
		RequestID: requestID,
		Request:   controlRequestBody{Subtype: "interrupt"},
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// controlResponse is the stream-json reply to a control_request from the
// CLI. It matches the success envelope of the Agent SDK.
type controlResponse struct {
	Type     string              `json:"type"`
	Response controlResponseBody `json:"response"`
}

type controlResponseBody struct {
	Subtype   string           `json:"subtype"`
	RequestID string           `json:"request_id"`
	Response  permissionResult `json:"response"`
}

// permissionResult is the decision payload for can_use_tool. An allow
// echoes the tool input back as updatedInput. A deny carries the reason.
type permissionResult struct {
	Behavior     string         `json:"behavior"`
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
	Message      string         `json:"message,omitempty"`
}

// buildPermissionResponse encodes one control_response line that answers
// a can_use_tool request. The line ends with a newline. The CLI blocks
// the tool call until this line arrives on stdin.
func buildPermissionResponse(
	requestID string,
	allow bool,
	input map[string]any,
	denyReason string,
) ([]byte, error) {
	result := permissionResult{Behavior: "deny", Message: denyReason}
	if allow {
		result = permissionResult{Behavior: "allow", UpdatedInput: input}
	}

	b, err := json.Marshal(controlResponse{
		Type: "control_response",
		Response: controlResponseBody{
			Subtype:   "success",
			RequestID: requestID,
			Response:  result,
		},
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// close stops the subprocess. A closed stdin lets the CLI drain its
// output. If the CLI does not exit within 3 s, [gracefulShutdown] sends
// SIGINT and then SIGKILL.
func (t *transport) close() error {
	t.shutdown()
	<-t.exitedCh
	return t.exitErrVal
}

// shutdown closes stdin. If the process does not exit after the grace
// period, shutdown signals it.
func (t *transport) shutdown() {
	_ = t.stdinPipe.Close()

	select {
	case <-t.exitedCh:
		return
	case <-time.After(3 * time.Second):
	}

	gracefulShutdown(t.cmd, t.exitedCh)
}

// buildArgs builds the CLI arguments for a persistent stream-json
// subprocess. It never uses the positional prompt argument, because the
// prompts arrive on stdin.
func buildArgs(cfg config) []string {
	a := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		// Stream content-block deltas as stream_event lines so the
		// parser can emit message_update events between message_start
		// and message_end.
		"--include-partial-messages",
	}

	if cfg.model != "" {
		a = append(a, "--model", cfg.model)
	}
	if effort := effortForThinkingLevel(cfg.thinkingLevel); effort != "" {
		a = append(a, "--effort", effort)
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
	if cfg.mcpConfig != "" {
		a = append(a, "--mcp-config", cfg.mcpConfig)
		if cfg.strictMCP {
			a = append(a, "--strict-mcp-config")
		}
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
		flag := "--append-system-prompt"
		if cfg.replacePrompt {
			flag = "--system-prompt"
		}
		a = append(a, flag, cfg.systemPrompt)
	}
	// The CLI names a session once and resumes it after that. Passing
	// --session-id for an ID it already holds is an error, so the flag
	// follows whether this run creates the session or continues it.
	if cfg.sessionID != "" {
		flag := "--resume"
		if cfg.newSession {
			flag = "--session-id"
		}
		a = append(a, flag, cfg.sessionID)
	}
	if len(cfg.beforeTool) > 0 {
		// Route the tool-approval questions of the CLI to this process
		// as can_use_tool control requests. The CLI then does not decide
		// by itself.
		a = append(a, "--permission-prompt-tool", "stdio")
	}
	if cfg.permissionMode != "" {
		a = append(a, "--permission-mode", cfg.permissionMode)
	}

	return a
}

// cleanEnv returns the current environment without the variables that
// stop Claude Code from starting. One example is the detection of a
// nested session.
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

// gracefulShutdown sends SIGINT and waits 3 seconds. It then sends
// SIGKILL. The done channel must close when the process exits.
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
