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
	"github.com/sonnes/pi-go/pkg/pubsub"
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

	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter

	exitedCh   chan struct{}
	exitedOnce sync.Once
	exitErrVal error
}

func newFakeTransport() *fakeTransport {
	r, w := io.Pipe()
	return &fakeTransport{
		pendingWrite: make(chan struct{}, 16),
		stdoutR:      r,
		stdoutW:      w,
		exitedCh:     make(chan struct{}),
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

// newTestAgent builds an Agent wired to a fake transport. The provided
// emitter is run in a goroutine after the first stdin write; it receives
// the transport so it can emit canned NDJSON lines.
func newTestAgent(
	t *testing.T,
	startErr error,
	emit func(ft *fakeTransport),
) (*Agent, *fakeTransport) {
	t.Helper()
	ft := newFakeTransport()
	a := New()
	a.newTransport = func(ctx context.Context, cfg config) (transportIface, error) {
		if startErr != nil {
			return nil, startErr
		}
		return ft, nil
	}
	if emit != nil {
		go func() {
			select {
			case <-ft.pendingWrite:
				emit(ft)
			case <-ft.exitedCh:
			}
		}()
	}
	return a, ft
}

// emitString returns an emitter that writes the given NDJSON then leaves
// the pipe open (pipe writes block only if reader stops — closing would
// EOF the scanner and drop any late turnDone delivery).
func emitString(s string) func(*fakeTransport) {
	return func(ft *fakeTransport) {
		ft.emit(s)
	}
}

// --- tests ---

func TestAgent_Send_SimpleText(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	ctx := context.Background()
	ch := a.Subscribe(ctx)
	err := a.Send(ctx, "hi")
	require.NoError(t, err)

	events := collectUntilAgentEnd(t, ch)

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventMessageStart, // user input
		agent.EventMessageEnd,
		agent.EventAgentStart,
		agent.EventTurnStart,
		agent.EventMessageStart, // assistant
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		agent.EventAgentEnd,
	}, types)

	last := events[len(events)-1]
	require.Len(t, last.Messages, 1)
	assert.Equal(t, "Hello!", last.Messages[0].Text())
	assert.Equal(t, 10, last.Usage.Input)
	assert.Equal(t, 5, last.Usage.Output)
	assert.NoError(t, last.Err)
}

func TestAgent_Send_WritesSDKUserMessageToStdin(t *testing.T) {
	a, ft := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	ctx := context.Background()
	require.NoError(t, a.Send(ctx, "ping"))
	_, err := a.Wait(ctx)
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

func TestAgent_Send_MultiTurn(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(multiTurnNDJSON))
	defer a.Close()

	ctx := context.Background()
	ch := a.Subscribe(ctx)
	require.NoError(t, a.Send(ctx, "read /tmp/foo"))

	events := collectUntilAgentEnd(t, ch)

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventMessageStart, // user input
		agent.EventMessageEnd,
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

func TestAgent_SessionID(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	ctx := context.Background()
	require.NoError(t, a.Send(ctx, "hi"))
	_, err := a.Wait(ctx)
	require.NoError(t, err)

	assert.Equal(t, "sess-1", a.SessionID())
}

func TestAgent_Messages_Accumulate(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	ctx := context.Background()
	require.NoError(t, a.Send(ctx, "hi"))
	_, err := a.Wait(ctx)
	require.NoError(t, err)

	msgs := a.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, agent.RoleUser, msgs[0].Role())
	assert.Equal(t, agent.RoleAssistant, msgs[1].Role())
}

func TestAgent_IsRunning(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	assert.False(t, a.IsRunning())

	ctx := context.Background()
	require.NoError(t, a.Send(ctx, "hi"))
	_, _ = a.Wait(ctx)
	assert.False(t, a.IsRunning())
}

func TestAgent_ConcurrentSend_Rejected(t *testing.T) {
	// Emitter never fires the result line, so the first turn stays open
	// until we force-close.
	a, ft := newTestAgent(t, nil, func(ft *fakeTransport) {
		ft.emit(`{"type":"system","subtype":"init","session_id":"sess"}` + "\n")
	})

	ctx := context.Background()
	require.NoError(t, a.Send(ctx, "first"))

	require.Eventually(t, a.IsRunning, time.Second, 5*time.Millisecond,
		"first Send should flip running=true",
	)

	err := a.Send(ctx, "second")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Close terminates the transport; awaitTurn sees exited and finishes.
	_ = ft.close()
	_, _ = a.Wait(ctx)
	a.Close()
}

func TestAgent_Send_StartError(t *testing.T) {
	a, _ := newTestAgent(t, errors.New("cli not found"), nil)
	defer a.Close()

	ctx := context.Background()
	ch := a.Subscribe(ctx)
	require.NoError(t, a.Send(ctx, "hi"))

	events := collectUntilAgentEnd(t, ch)

	for _, e := range events {
		assert.NotEqual(t, agent.EventAgentStart, e.Type,
			"agent_start must not fire when transport fails to start",
		)
	}
	last := events[len(events)-1]
	assert.Equal(t, agent.EventAgentEnd, last.Type)
	require.Error(t, last.Err)
	assert.Contains(t, last.Err.Error(), "cli not found")

	_, err := a.Wait(ctx)
	require.Error(t, err)
	assert.False(t, a.IsRunning())
}

func TestAgent_Continue_ReturnsError(t *testing.T) {
	a := New()
	err := a.Continue(context.Background())
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

	ctx := context.Background()
	require.NoError(t, a.Send(ctx, "test"))
	msgs, err := a.Wait(ctx)
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

func TestAgent_Send_FullToolLoop(t *testing.T) {
	a, _ := newTestAgent(t, nil, emitString(toolLoopNDJSON))
	defer a.Close()

	ctx := context.Background()
	ch := a.Subscribe(ctx)
	require.NoError(t, a.Send(ctx, "read /tmp/foo"))

	events := collectUntilAgentEnd(t, ch)

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventMessageStart, // user input
		agent.EventMessageEnd,
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

func TestAgent_Send_ErrorResult(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"result","subtype":"error","result":"Rate limited","is_error":true}
`
	a, _ := newTestAgent(t, nil, emitString(ndjson))
	defer a.Close()

	ctx := context.Background()
	require.NoError(t, a.Send(ctx, "hi"))
	_, err := a.Wait(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rate limited")
}

func TestAgent_Send_TwoTurnsReuseTransport(t *testing.T) {
	turn1 := `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"one"}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"one"}
`
	turn2 := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"two"}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"two"}
`
	ft := newFakeTransport()
	var factoryCalls int
	a := New()
	a.newTransport = func(ctx context.Context, cfg config) (transportIface, error) {
		factoryCalls++
		return ft, nil
	}

	// Emitter: first write → turn1 output, second write → turn2 output.
	go func() {
		for i, body := range []string{turn1, turn2} {
			select {
			case <-ft.pendingWrite:
			case <-ft.exitedCh:
				return
			}
			_ = i
			ft.emit(body)
		}
	}()

	ctx := context.Background()

	require.NoError(t, a.Send(ctx, "first"))
	_, err := a.Wait(ctx)
	require.NoError(t, err)

	require.NoError(t, a.Send(ctx, "second"))
	_, err = a.Wait(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, factoryCalls, "transport must be reused across turns")
	assert.Len(t, ft.writes(), 2)

	a.Close()
}

func TestAgent_SendMessages_RichContent(t *testing.T) {
	a, ft := newTestAgent(t, nil, emitString(simpleTextNDJSON))
	defer a.Close()

	ctx := context.Background()
	msg := ai.UserImageMessage("describe this",
		ai.Image{Data: "AAA=", MimeType: "image/png"},
	)
	err := a.SendMessages(ctx, agent.NewLLMMessage(msg))
	require.NoError(t, err)
	_, err = a.Wait(ctx)
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

func collectUntilAgentEnd(t *testing.T, ch <-chan pubsub.Event[agent.Event]) []agent.Event {
	t.Helper()
	var events []agent.Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case pe, ok := <-ch:
			if !ok {
				t.Fatalf("subscription channel closed before agent_end")
			}
			evt := pe.Payload()
			events = append(events, evt)
			if evt.Type == agent.EventAgentEnd {
				return events
			}
		case <-timeout:
			t.Fatalf("timed out waiting for agent_end; saw %d events: %v", len(events), eventTypes(events))
		}
	}
}
