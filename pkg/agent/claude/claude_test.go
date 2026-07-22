package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NDJSON fixtures ---

const simpleTextNDJSON = `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello!"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}}
{"type":"result","subtype":"success","result":"Hello!","session_id":"sess-1","usage":{"input_tokens":10,"output_tokens":5}}
`

const multiTurnNDJSON = `{"type":"system","subtype":"init","session_id":"sess-2"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me read that."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/foo"}}],"stop_reason":"tool_use","usage":{"input_tokens":20,"output_tokens":10}}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"The file says hello."}],"stop_reason":"end_turn","usage":{"input_tokens":30,"output_tokens":15}}}
{"type":"result","subtype":"success","result":"The file says hello.","session_id":"sess-2","usage":{"input_tokens":50,"output_tokens":25}}
`

// --- fake transport ---

// fakeTransport is an in-memory transportIface. It connects stdout via an
// io.Pipe so tests can stream NDJSON on demand; stdin writes are captured
// for inspection.
type fakeTransport struct {
	mu           sync.Mutex
	stdinWrites  [][]byte
	pendingWrite chan struct{} // unblocks a goroutine waiting to observe a write

	controlWrites  [][]byte
	controlWritten chan struct{} // signals a captured control response

	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter

	exitedCh   chan struct{}
	exitedOnce sync.Once
	exitErrVal error

	interruptedCh   chan struct{}
	interruptedOnce sync.Once
}

func newFakeTransport() *fakeTransport {
	r, w := io.Pipe()
	return &fakeTransport{
		pendingWrite:   make(chan struct{}, 16),
		controlWritten: make(chan struct{}, 16),
		stdoutR:        r,
		stdoutW:        w,
		exitedCh:       make(chan struct{}),
		interruptedCh:  make(chan struct{}),
	}
}

// interrupt records the abort request and unblocks anyone waiting on
// interruptedCh, without terminating the subprocess.
func (f *fakeTransport) interrupt() error {
	f.interruptedOnce.Do(func() { close(f.interruptedCh) })
	return nil
}

// interrupted reports whether interrupt() was called.
func (f *fakeTransport) interrupted() bool {
	select {
	case <-f.interruptedCh:
		return true
	default:
		return false
	}
}

func (f *fakeTransport) writeUserMessage(line []byte) error {
	select {
	case <-f.exitedCh:
		return errors.New("fake: closed")
	default:
	}
	f.mu.Lock()
	f.stdinWrites = append(f.stdinWrites, append([]byte(nil), line...))
	f.mu.Unlock()
	select {
	case f.pendingWrite <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeTransport) writeControlResponse(line []byte) error {
	f.mu.Lock()
	f.controlWrites = append(f.controlWrites, append([]byte(nil), line...))
	f.mu.Unlock()
	select {
	case f.controlWritten <- struct{}{}:
	default:
	}
	return nil
}

// controlResponses returns a copy of captured control response writes.
func (f *fakeTransport) controlResponses() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.controlWrites))
	for i, w := range f.controlWrites {
		out[i] = append([]byte(nil), w...)
	}
	return out
}

func (f *fakeTransport) stdout() io.Reader       { return f.stdoutR }
func (f *fakeTransport) exited() <-chan struct{} { return f.exitedCh }
func (f *fakeTransport) exitErr() error          { return f.exitErrVal }
func (f *fakeTransport) close() error {
	f.exitedOnce.Do(func() {
		close(f.exitedCh)
		_ = f.stdoutW.Close()
	})
	return f.exitErrVal
}

// emit writes NDJSON to stdout. Blocks until the reader consumes enough to
// make room (pipe has no internal buffering).
func (f *fakeTransport) emit(s string) {
	_, _ = f.stdoutW.Write([]byte(s))
}

// writes returns a copy of captured stdin writes.
func (f *fakeTransport) writes() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.stdinWrites))
	for i, w := range f.stdinWrites {
		out[i] = append([]byte(nil), w...)
	}
	return out
}

// writeCount returns how many stdin writes were captured.
func (f *fakeTransport) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.stdinWrites)
}

// newTestAgent builds an Agent wired to a fake transport. The emitter
// runs in a goroutine and receives the transport so it can choose when
// to emit lines (typically after waiting on [fakeTransport.pendingWrite]
// to mirror the real Claude CLI, which only emits its `system/init`
// line and any subsequent output in response to a stdin user message).
func newTestAgent(
	t *testing.T,
	startErr error,
	emit func(ft *fakeTransport),
) (*Agent, *fakeTransport) {
	t.Helper()
	ft := newFakeTransport()
	a := New(ai.Model{})
	a.newTransport = func(ctx context.Context, cfg config) (transportIface, error) {
		if startErr != nil {
			return nil, startErr
		}
		return ft, nil
	}
	if emit != nil {
		go emit(ft)
	}
	return a, ft
}

// emitString returns an emitter that emits the given NDJSON only after
// the agent writes a user message to stdin — matching the real Claude
// CLI which is silent until it receives input and then streams its
// `system/init` line followed by the assistant/result blocks.
func emitString(s string) func(*fakeTransport) {
	return func(ft *fakeTransport) {
		select {
		case <-ft.pendingWrite:
		case <-ft.exitedCh:
			return
		}
		ft.emit(s)
	}
}

// --- tests ---

func TestAgent_Run_SimpleText(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("hi")))
	require.NoError(t, err)

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		agent.EventTurnStart,
		agent.EventMessageStart, // assistant
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		agent.EventAgentEnd,
	}, types)

	assert.Equal(t, "sess-1", events[0].SessionID,
		"agent_start carries the subprocess session_id",
	)

	last := events[len(events)-1]
	require.Len(t, last.Messages, 1)
	assert.Equal(t, "Hello!", last.Messages[0].Text())
	assert.Equal(t, 10, last.Usage.Input)
	assert.Equal(t, 5, last.Usage.Output)
}

func TestAgent_Run_WritesSDKUserMessageToStdin(t *testing.T) {
	a, ft := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("ping")).Wait()
	require.NoError(t, err)

	writes := ft.writes()
	require.Len(t, writes, 1)

	var decoded sdkUserMessage
	require.NoError(t, json.Unmarshal(bytes.TrimRight(writes[0], "\n"), &decoded))
	assert.Equal(t, "user", decoded.Type)
	assert.Equal(t, "user", decoded.Message.Role)

	var content string
	require.NoError(t, json.Unmarshal(decoded.Message.Content, &content))
	assert.Equal(t, "ping", content)
}

func TestAgent_Run_MultiTurn(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(multiTurnNDJSON))
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("read /tmp/foo")))
	require.NoError(t, err)

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventToolExecutionStart,
		agent.EventTurnEnd,
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		agent.EventAgentEnd,
	}, types)

	last := events[len(events)-1]
	require.Len(t, last.Messages, 2)
	assert.Equal(t, ai.StopReasonToolUse, last.Messages[0].StopReason)
	assert.Equal(t, "The file says hello.", last.Messages[1].Text())
}

// permissionNDJSON opens a turn whose Bash call requires approval: the
// CLI (launched with --permission-prompt-tool stdio) emits a
// control_request with subtype can_use_tool and blocks until the
// control_response arrives on stdin.
const permissionRequestNDJSON = `{"type":"system","subtype":"init","session_id":"sess-p"}
{"type":"control_request","request_id":"cr-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"},"tool_use_id":"tu-1"}}
`

const permissionResultNDJSON = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"Done.","session_id":"sess-p"}
`

// newPermissionTestAgent wires an Agent whose fake CLI asks for
// permission, waits for the agent's control_response, then finishes
// the turn — mirroring the real CLI's blocking behavior.
func newPermissionTestAgent(t *testing.T, hook agent.Hook) (*Agent, *fakeTransport) {
	t.Helper()
	ft := newFakeTransport()
	a := New(ai.Model{}, agent.WithHook(agent.HookBeforeTool, hook))
	a.newTransport = func(context.Context, config) (transportIface, error) {
		return ft, nil
	}
	go func() {
		select {
		case <-ft.pendingWrite:
		case <-ft.exitedCh:
			return
		}
		ft.emit(permissionRequestNDJSON)
		select {
		case <-ft.controlWritten:
		case <-ft.exitedCh:
			return
		}
		ft.emit(permissionResultNDJSON)
	}()
	return a, ft
}

func TestAgent_Run_CanUseToolAllow(t *testing.T) {
	var gotCall ai.ToolCall
	hook := func(_ context.Context, in *agent.HookInput) (*agent.HookOutput, error) {
		if in.ToolCall != nil {
			gotCall = *in.ToolCall
		}
		return nil, nil
	}

	a, ft := newPermissionTestAgent(t, hook)
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("run ls")).Wait()
	require.NoError(t, err)

	// The hook saw the CLI's tool call.
	assert.Equal(t, "Bash", gotCall.Name)
	assert.Equal(t, "tu-1", gotCall.ID)
	assert.Equal(t, map[string]any{"command": "ls"}, gotCall.Arguments)

	// The agent answered allow, echoing the input.
	responses := ft.controlResponses()
	require.Len(t, responses, 1)
	var resp struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Response  struct {
				Behavior     string         `json:"behavior"`
				UpdatedInput map[string]any `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	require.NoError(t, json.Unmarshal(bytes.TrimRight(responses[0], "\n"), &resp))
	assert.Equal(t, "control_response", resp.Type)
	assert.Equal(t, "cr-1", resp.Response.RequestID)
	assert.Equal(t, "allow", resp.Response.Response.Behavior)
	assert.Equal(t, map[string]any{"command": "ls"}, resp.Response.Response.UpdatedInput)
}

func TestAgent_Run_CanUseToolDeny(t *testing.T) {
	hook := func(context.Context, *agent.HookInput) (*agent.HookOutput, error) {
		return &agent.HookOutput{Deny: true, DenyReason: "denied by user"}, nil
	}

	a, ft := newPermissionTestAgent(t, hook)
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("run ls")).Wait()
	require.NoError(t, err, "a denied tool still completes the turn")

	responses := ft.controlResponses()
	require.Len(t, responses, 1)
	var resp struct {
		Response struct {
			RequestID string `json:"request_id"`
			Response  struct {
				Behavior string `json:"behavior"`
				Message  string `json:"message"`
			} `json:"response"`
		} `json:"response"`
	}
	require.NoError(t, json.Unmarshal(bytes.TrimRight(responses[0], "\n"), &resp))
	assert.Equal(t, "cr-1", resp.Response.RequestID)
	assert.Equal(t, "deny", resp.Response.Response.Behavior)
	assert.Equal(t, "denied by user", resp.Response.Response.Message)
}

// A hook error denies execution with the error text — mirroring the
// Default agent's before_tool semantics.
func TestAgent_Run_CanUseToolHookError(t *testing.T) {
	hook := func(context.Context, *agent.HookInput) (*agent.HookOutput, error) {
		return nil, errors.New("gate exploded")
	}

	a, ft := newPermissionTestAgent(t, hook)
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("run ls")).Wait()
	require.NoError(t, err)

	responses := ft.controlResponses()
	require.Len(t, responses, 1)
	assert.Contains(t, string(responses[0]), `"behavior":"deny"`)
	assert.Contains(t, string(responses[0]), "gate exploded")
}

// streamingNDJSON mirrors --include-partial-messages output: stream_event
// lines with content-block deltas interleaved with the assistant line.
const streamingNDJSON = `{"type":"system","subtype":"init","session_id":"sess-s"}
{"type":"stream_event","event":{"type":"message_start","message":{"role":"assistant","content":[]}}}
{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo!"}}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello!"}],"stop_reason":"end_turn"}}
{"type":"stream_event","event":{"type":"content_block_stop","index":0}}
{"type":"stream_event","event":{"type":"message_stop"}}
{"type":"result","subtype":"success","result":"Hello!","session_id":"sess-s","usage":{"input_tokens":10,"output_tokens":5}}
`

func TestAgent_Run_StreamingDeltas(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(streamingNDJSON))
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("hi")))
	require.NoError(t, err)

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		agent.EventTurnStart,
		agent.EventMessageStart, // empty message, opened by content_block_start
		agent.EventMessageUpdate,
		agent.EventMessageUpdate,
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		agent.EventAgentEnd,
	}, types)

	var streamed string
	for _, e := range events {
		if e.Type == agent.EventMessageUpdate {
			require.NotNil(t, e.AssistantEvent)
			streamed += e.AssistantEvent.Delta
		}
	}
	assert.Equal(t, "Hello!", streamed,
		"concatenated deltas must reproduce the final text")

	last := events[len(events)-1]
	require.Len(t, last.Messages, 1)
	assert.Equal(t, "Hello!", last.Messages[0].Text())
}

// Canceling the Run context interrupts the in-flight turn (writes the
// control request via the transport) — the persistent subprocess stays
// alive for the next Run.
func TestAgent_Run_CancelInterruptsTurnKeepsSubprocess(t *testing.T) {
	ft := newFakeTransport()
	a := New(ai.Model{})
	a.newTransport = func(_ context.Context, _ config) (transportIface, error) { return ft, nil }
	defer a.Close()

	go func() {
		select {
		case <-ft.pendingWrite:
		case <-ft.exitedCh:
			return
		}
		ft.emit(`{"type":"system","subtype":"init","session_id":"sess-i"}` + "\n")
		// Stay mid-turn until the agent asks for an interrupt, then emit a
		// result line so the turn closes — as the real CLI does on interrupt.
		select {
		case <-ft.interruptedCh:
		case <-ft.exitedCh:
			return
		}
		ft.emit(`{"type":"result","subtype":"error_during_execution","result":"Interrupted","session_id":"sess-i"}` + "\n")
	}()

	ctx, cancel := context.WithCancel(context.Background())
	s := a.Run(ctx, ai.UserMessage("hi"))

	require.Eventually(t, func() bool { return ft.writeCount() == 1 },
		time.Second, 5*time.Millisecond,
		"Run should write the user line",
	)

	cancel()

	_, err := s.Wait()
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, ft.interrupted(), "cancel must request a transport interrupt")

	select {
	case <-ft.exited():
		t.Fatal("cancel terminated the persistent subprocess")
	default:
	}
}

func TestAgent_SessionID(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("hi")).Wait()
	require.NoError(t, err)

	assert.Equal(t, "sess-1", a.SessionID())
}

func TestAgent_Messages_Accumulate(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("hi")).Wait()
	require.NoError(t, err)

	msgs := a.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, ai.RoleUser, msgs[0].Role)
	assert.Equal(t, ai.RoleAssistant, msgs[1].Role)
}

func TestAgent_Run_ConcurrentRejected(t *testing.T) {
	// Emitter never fires the result line, so the first turn stays open
	// until we force-close.
	a, ft := newTestAgent(t, nil, func(ft *fakeTransport) {
		ft.emit(`{"type":"system","subtype":"init","session_id":"sess"}` + "\n")
	})

	ctx := context.Background()
	first := a.Run(ctx, ai.UserMessage("first"))

	require.Eventually(t, func() bool { return ft.writeCount() == 1 },
		time.Second, 5*time.Millisecond,
		"first Run should write its user line",
	)

	_, err := a.Run(ctx, ai.UserMessage("second")).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Close terminates the transport; awaitTurn sees exited and finishes.
	_ = ft.close()
	_, err = first.Wait()
	require.Error(t, err)
	a.Close()
}

func TestAgent_Run_StartError(t *testing.T) {
	a, _ := newTestAgent(t, errors.New("cli not found"), nil)
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("hi")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli not found")

	for _, e := range events {
		assert.NotEqual(t, agent.EventAgentStart, e.Type,
			"agent_start must not fire when transport fails to start",
		)
	}
}

func TestAgent_Run_NoMessages_ReturnsError(t *testing.T) {
	a := New(ai.Model{})
	_, err := a.Run(context.Background()).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestAgent_MalformedNDJSON(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init","session_id":"s1"}
{invalid json here}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Still works"}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"Still works","usage":{"input_tokens":5,"output_tokens":3}}
`
	a, _ := newTestAgent(t, nil, emitString(ndjson))
	defer a.Close()

	msgs, err := a.Run(context.Background(), ai.UserMessage("test")).Wait()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Still works", msgs[0].Text())
}

// toolLoopNDJSON simulates: system → assistant (tool_use) → user (tool_result) → assistant (final) → result
const toolLoopNDJSON = `{"type":"system","subtype":"init","session_id":"sess-3"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me read that."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/foo"}}],"stop_reason":"tool_use"}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"package main"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"It's a Go file."}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"It's a Go file.","usage":{"input_tokens":100,"output_tokens":30},"cost_usd":0.002}
`

func TestAgent_Run_FullToolLoop(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(toolLoopNDJSON))
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("read /tmp/foo")))
	require.NoError(t, err)

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		agent.EventTurnStart,
		agent.EventMessageStart, // assistant with tool_use
		agent.EventMessageEnd,
		agent.EventToolExecutionStart,
		agent.EventToolExecutionEnd, // tool result
		agent.EventMessageStart,     // tool result message
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		agent.EventAgentEnd,
	}, types)

	last := events[len(events)-1]
	require.Len(t, last.Messages, 3)
	assert.Equal(t, ai.StopReasonToolUse, last.Messages[0].StopReason)
	assert.Equal(t, ai.RoleToolResult, last.Messages[1].Role)
	assert.Equal(t, "It's a Go file.", last.Messages[2].Text())
	assert.Equal(t, 100, last.Usage.Input)
	assert.Equal(t, 30, last.Usage.Output)
	assert.InDelta(t, 0.002, last.Usage.Cost.Total, 0.0001)

	var turnEnds []agent.Event
	for _, e := range events {
		if e.Type == agent.EventTurnEnd {
			turnEnds = append(turnEnds, e)
		}
	}
	require.Len(t, turnEnds, 2)
	require.Len(t, turnEnds[0].ToolResults, 1)
	assert.Equal(t, "t1", turnEnds[0].ToolResults[0].ToolCallID)
}

func TestAgent_Run_ErrorResult(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"result","subtype":"error","result":"Rate limited","is_error":true}
`
	a, _ := newTestAgent(t, nil, emitString(ndjson))
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("hi")).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rate limited")
}

func TestAgent_Run_TwoTurnsReuseTransport(t *testing.T) {
	turn1 := `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"one"}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"one"}
`
	turn2 := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"two"}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"two"}
`
	ft := newFakeTransport()
	var factoryCalls int
	a := New(ai.Model{})
	a.newTransport = func(ctx context.Context, cfg config) (transportIface, error) {
		factoryCalls++
		return ft, nil
	}

	// Mimic the real CLI: silent until the first stdin write. The
	// init line accompanies the first turn's assistant/result block.
	go func() {
		for _, body := range []string{turn1, turn2} {
			select {
			case <-ft.pendingWrite:
			case <-ft.exitedCh:
				return
			}
			ft.emit(body)
		}
	}()

	ctx := context.Background()

	_, err := a.Run(ctx, ai.UserMessage("first")).Wait()
	require.NoError(t, err)

	_, err = a.Run(ctx, ai.UserMessage("second")).Wait()
	require.NoError(t, err)

	assert.Equal(t, 1, factoryCalls, "transport must be reused across turns")
	assert.Len(t, ft.writes(), 2)

	require.NoError(t, a.Close())
}

func TestAgent_Run_RichContent(t *testing.T) {
	a, ft := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	msg := ai.UserImageMessage("describe this",
		ai.Image{Data: "AAA=", MimeType: "image/png"},
	)
	_, err := a.Run(context.Background(), msg).Wait()
	require.NoError(t, err)

	writes := ft.writes()
	require.Len(t, writes, 1)

	var decoded sdkUserMessage
	require.NoError(t, json.Unmarshal(bytes.TrimRight(writes[0], "\n"), &decoded))

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal(decoded.Message.Content, &blocks))
	require.Len(t, blocks, 2)
	assert.Equal(t, "text", blocks[0]["type"])
	assert.Equal(t, "image", blocks[1]["type"])
}

// --- helpers ---

func eventTypes(events []agent.Event) []agent.EventType {
	types := make([]agent.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

// collectRun drains a run's stream with a watchdog timeout, returning
// its events and terminal error.
func collectRun(t *testing.T, s *agent.Stream) ([]agent.Event, error) {
	t.Helper()

	type outcome struct {
		events []agent.Event
		err    error
	}
	done := make(chan outcome, 1)

	go func() {
		var events []agent.Event
		for e, err := range s.Events() {
			if err != nil {
				done <- outcome{events, err}
				return
			}
			events = append(events, e)
		}
		done <- outcome{events, nil}
	}()

	select {
	case o := <-done:
		return o.events, o.err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the run to finish")
		return nil, nil
	}
}
