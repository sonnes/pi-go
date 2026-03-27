package claude

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

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

// --- tests ---

// stubSend replaces Agent.send for testing by injecting a canned
// NDJSON response or error. Returns a restore func.
func stubSend(a *Agent, output string, sendErr error) (lastArgs func() sendArgs, restore func()) {
	var captured sendArgs
	orig := a.sendFn
	a.sendFn = func(_ context.Context, args sendArgs) (io.ReadCloser, func() error, error) {
		captured = args
		if sendErr != nil {
			return nil, nil, sendErr
		}
		r := io.NopCloser(strings.NewReader(output))
		return r, func() error { return nil }, nil
	}
	return func() sendArgs { return captured }, func() { a.sendFn = orig }
}

func TestAgent_Send_SimpleText(t *testing.T) {
	a := New()
	_, restore := stubSend(a, simpleTextNDJSON, nil)
	defer restore()

	ctx := context.Background()
	ch := a.Subscribe(ctx)
	err := a.Send(ctx, "hi")
	require.NoError(t, err)

	var events []agent.Event
	for pe := range ch {
		evt := pe.Payload()
		events = append(events, evt)
		if evt.Type == agent.EventAgentEnd {
			break
		}
	}

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		agent.EventMessageStart, // user input
		agent.EventMessageEnd,
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

func TestAgent_Send_MultiTurn(t *testing.T) {
	a := New()
	_, restore := stubSend(a, multiTurnNDJSON, nil)
	defer restore()

	ctx := context.Background()
	ch := a.Subscribe(ctx)
	err := a.Send(ctx, "read /tmp/foo")
	require.NoError(t, err)

	var events []agent.Event
	for pe := range ch {
		evt := pe.Payload()
		events = append(events, evt)
		if evt.Type == agent.EventAgentEnd {
			break
		}
	}

	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		agent.EventMessageStart, // user input
		agent.EventMessageEnd,
		// Turn 1: assistant with tool_use (turn stays open)
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventToolExecutionStart,
		// Next assistant closes turn 1, opens turn 2
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
	a := New()
	_, restore := stubSend(a, simpleTextNDJSON, nil)
	defer restore()

	ctx := context.Background()
	err := a.Send(ctx, "hi")
	require.NoError(t, err)
	_, err = a.Wait(ctx)
	require.NoError(t, err)

	assert.Equal(t, "sess-1", a.SessionID())
}

func TestAgent_Messages_Accumulate(t *testing.T) {
	a := New()
	_, restore := stubSend(a, simpleTextNDJSON, nil)
	defer restore()

	ctx := context.Background()
	err := a.Send(ctx, "hi")
	require.NoError(t, err)
	_, err = a.Wait(ctx)
	require.NoError(t, err)

	msgs := a.Messages()
	require.Len(t, msgs, 2) // user + assistant
	assert.Equal(t, agent.RoleUser, msgs[0].Role())
	assert.Equal(t, agent.RoleAssistant, msgs[1].Role())
}

func TestAgent_IsRunning(t *testing.T) {
	a := New()
	_, restore := stubSend(a, simpleTextNDJSON, nil)
	defer restore()

	assert.False(t, a.IsRunning())

	ctx := context.Background()
	err := a.Send(ctx, "hi")
	require.NoError(t, err)
	_, _ = a.Wait(ctx)
	assert.False(t, a.IsRunning())
}

func TestAgent_ConcurrentSend_Rejected(t *testing.T) {
	blocker := make(chan struct{})
	a := New()
	a.sendFn = func(_ context.Context, _ sendArgs) (io.ReadCloser, func() error, error) {
		<-blocker
		r := io.NopCloser(strings.NewReader(simpleTextNDJSON))
		return r, func() error { return nil }, nil
	}

	err := a.Send(context.Background(), "first")
	require.NoError(t, err)

	// Spin until the goroutine sets running=true.
	for i := 0; i < 100; i++ {
		if a.IsRunning() {
			break
		}
	}

	err = a.Send(context.Background(), "second")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	close(blocker)
}

func TestAgent_SendError(t *testing.T) {
	a := New()
	_, restore := stubSend(a, "", fmt.Errorf("cli not found"))
	defer restore()

	ctx := context.Background()
	err := a.Send(ctx, "hi")
	require.NoError(t, err)
	_, err = a.Wait(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli not found")
	assert.False(t, a.IsRunning())
}

func TestAgent_Continue_NoSession(t *testing.T) {
	a := New()

	err := a.Continue(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no session")
}

func TestAgent_Continue_WithSession(t *testing.T) {
	a := New()
	lastArgs, restore := stubSend(a, simpleTextNDJSON, nil)
	defer restore()

	ctx := context.Background()
	err := a.Send(ctx, "hi")
	require.NoError(t, err)
	_, err = a.Wait(ctx)
	require.NoError(t, err)
	require.Equal(t, "sess-1", a.SessionID())

	err = a.Continue(ctx)
	require.NoError(t, err)
	_, err = a.Wait(ctx)
	require.NoError(t, err)

	assert.True(t, lastArgs().resume)
	assert.Equal(t, "sess-1", lastArgs().sessionID)
}

func TestAgent_MalformedNDJSON(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init","session_id":"s1"}
{invalid json here}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Still works"}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"Still works","usage":{"input_tokens":5,"output_tokens":3}}
`
	a := New()
	_, restore := stubSend(a, ndjson, nil)
	defer restore()

	ctx := context.Background()
	err := a.Send(ctx, "test")
	require.NoError(t, err)
	msgs, err := a.Wait(ctx)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Still works", msgs[0].Text())
}

func TestAgent_EmptyOutput(t *testing.T) {
	a := New()
	_, restore := stubSend(a, "", nil)
	defer restore()

	ctx := context.Background()
	err := a.Send(ctx, "hi")
	require.NoError(t, err)
	msgs, err := a.Wait(ctx)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

// toolLoopNDJSON simulates: system → assistant (tool_use) → user (tool_result) → assistant (final) → result
const toolLoopNDJSON = `{"type":"system","subtype":"init","session_id":"sess-3"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me read that."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/foo"}}],"stop_reason":"tool_use"}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"package main"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"It's a Go file."}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"It's a Go file.","usage":{"input_tokens":100,"output_tokens":30},"cost_usd":0.002}
`

func TestAgent_Send_FullToolLoop(t *testing.T) {
	a := New()
	_, restore := stubSend(a, toolLoopNDJSON, nil)
	defer restore()

	ctx := context.Background()
	ch := a.Subscribe(ctx)
	err := a.Send(ctx, "read /tmp/foo")
	require.NoError(t, err)

	var events []agent.Event
	for pe := range ch {
		evt := pe.Payload()
		events = append(events, evt)
		if evt.Type == agent.EventAgentEnd {
			break
		}
	}

	//
	// Verify the complete event sequence matches what a consumer
	// would see from a real Claude Code subprocess with tool use.
	//
	// Default agent emits a similar sequence (see default.go):
	//   agent_start
	//     message_start/end (user input)
	//     turn_start
	//       message_start/update*/end (assistant)
	//       tool_execution_start/update/end (per tool)
	//       message_start/end (tool results)
	//     turn_end
	//     turn_start
	//       message_start/end (final assistant)
	//     turn_end
	//   agent_end
	//
	types := eventTypes(events)
	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		// User input
		agent.EventMessageStart,
		agent.EventMessageEnd,
		// Turn 1: assistant calls a tool (turn stays open)
		agent.EventTurnStart,
		agent.EventMessageStart, // assistant with tool_use
		agent.EventMessageEnd,
		agent.EventToolExecutionStart, // tool call observed
		// Tool result arrives inside the same turn
		agent.EventToolExecutionEnd, // tool result observed
		agent.EventMessageStart,     // tool result message
		agent.EventMessageEnd,
		// Turn 1 closes when next assistant arrives
		agent.EventTurnEnd, // carries ToolResults
		// Turn 2: final assistant response
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		// Done
		agent.EventAgentEnd,
	}, types)

	// Verify agent_end fields.
	last := events[len(events)-1]
	require.Len(t, last.Messages, 3) // assistant + tool_result + assistant
	assert.Equal(t, ai.StopReasonToolUse, last.Messages[0].StopReason)
	assert.Equal(t, ai.RoleToolResult, last.Messages[1].Role)
	assert.Equal(t, "It's a Go file.", last.Messages[2].Text())
	assert.Equal(t, 100, last.Usage.Input)
	assert.Equal(t, 30, last.Usage.Output)
	assert.InDelta(t, 0.002, last.Usage.Cost.Total, 0.0001)
	assert.NoError(t, last.Err)

	// The first turn_end (tool-use turn) carries tool results.
	var turnEnds []agent.Event
	for _, e := range events {
		if e.Type == agent.EventTurnEnd {
			turnEnds = append(turnEnds, e)
		}
	}
	require.Len(t, turnEnds, 2)
	require.Len(t, turnEnds[0].ToolResults, 1)
	assert.Equal(t, "t1", turnEnds[0].ToolResults[0].ToolCallID)

	// Verify accumulated messages.
	msgs := a.Messages()
	require.Len(t, msgs, 4) // user + assistant + tool_result + assistant
	assert.Equal(t, agent.RoleUser, msgs[0].Role())
	assert.Equal(t, agent.RoleAssistant, msgs[1].Role())
	assert.Equal(t, agent.RoleToolResult, msgs[2].Role())
	assert.Equal(t, agent.RoleAssistant, msgs[3].Role())
}

func TestAgent_Send_ErrorResult(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"result","subtype":"error","result":"Rate limited","is_error":true}
`
	a := New()
	_, restore := stubSend(a, ndjson, nil)
	defer restore()

	ctx := context.Background()
	err := a.Send(ctx, "hi")
	require.NoError(t, err)
	_, err = a.Wait(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rate limited")
}

// --- helpers ---

func eventTypes(events []agent.Event) []agent.EventType {
	types := make([]agent.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}
