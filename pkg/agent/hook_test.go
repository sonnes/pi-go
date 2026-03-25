package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test 1: Middleware passes through — calls next, tool runs normally.
func TestMiddleware_PassesThrough(t *testing.T) {
	var called bool
	mw := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		called = true
		return next(ctx)
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "hello"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(testModel(), WithTools(echoTool()), WithMiddleware(mw))
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.True(t, called, "middleware should have been called")

	toolResult := findToolResult(t, msgs)
	assert.False(t, toolResult.IsError)
}

// Test 2: Middleware blocks execution — returns without calling next.
func TestMiddleware_BlocksExecution(t *testing.T) {
	var toolRan bool
	blockingTool := ai.DefineTool[toolInput, string](
		"guarded",
		"guarded tool",
		func(_ context.Context, in toolInput) (string, error) {
			toolRan = true
			return in.Input, nil
		},
	)

	mw := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		return ai.NewErrorResult(call.ID, "blocked by policy"), nil
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "guarded",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("ok", ai.Usage{}),
	)

	a := New(testModel(), WithTools(blockingTool), WithMiddleware(mw))
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.False(t, toolRan, "tool should not have been called")

	toolResult := findToolResult(t, msgs)
	assert.True(t, toolResult.IsError)
	assert.Contains(t, toolResult.Content[0].(ai.Text).Text, "blocked by policy")
}

// Test 3: Middleware modifies result.
func TestMiddleware_ModifiesResult(t *testing.T) {
	mw := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		result, err := next(ctx)
		if err != nil {
			return result, err
		}
		result.Content = "modified"
		return result, nil
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "original"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(testModel(), WithTools(echoTool()), WithMiddleware(mw))
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	toolResult := findToolResult(t, msgs)
	assert.False(t, toolResult.IsError)
	text, ok := ai.AsContent[ai.Text](toolResult.Content[0])
	require.True(t, ok)
	assert.Equal(t, "modified", text.Text)
}

// Test 4: Middleware returns error.
func TestMiddleware_ReturnsError(t *testing.T) {
	mw := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		return ai.ToolResult{}, assert.AnError
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("ok", ai.Usage{}),
	)

	a := New(testModel(), WithTools(echoTool()), WithMiddleware(mw))
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err) // Agent-level error is non-fatal (tool error).

	toolResult := findToolResult(t, msgs)
	assert.True(t, toolResult.IsError)
}

// Test 5: Context flows from middleware to tool.
func TestMiddleware_ContextFlows(t *testing.T) {
	type ctxKey struct{}

	var toolSawValue string
	ctxTool := ai.DefineTool[toolInput, string](
		"ctx_tool",
		"reads context",
		func(ctx context.Context, in toolInput) (string, error) {
			if v, ok := ctx.Value(ctxKey{}).(string); ok {
				toolSawValue = v
			}
			return "ok", nil
		},
	)

	mw := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		ctx = context.WithValue(ctx, ctxKey{}, "from-middleware")
		return next(ctx)
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "ctx_tool",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(testModel(), WithTools(ctxTool), WithMiddleware(mw))
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, "from-middleware", toolSawValue)
}

// Test 6: Chain order — m1 wraps m2 wraps tool.
func TestMiddleware_ChainOrder(t *testing.T) {
	var order []string

	m1 := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		order = append(order, "m1-before")
		result, err := next(ctx)
		order = append(order, "m1-after")
		return result, err
	}

	m2 := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		order = append(order, "m2-before")
		result, err := next(ctx)
		order = append(order, "m2-after")
		return result, err
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(testModel(), WithTools(echoTool()), WithMiddleware(m1, m2))
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, []string{"m1-before", "m2-before", "m2-after", "m1-after"}, order)
}

// Test 7: Chain early block — first middleware blocks, second never called.
func TestMiddleware_ChainEarlyBlock(t *testing.T) {
	var m2Called bool

	m1 := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		return ai.NewErrorResult(call.ID, "blocked"), nil
	}

	m2 := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		m2Called = true
		return next(ctx)
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("ok", ai.Usage{}),
	)

	a := New(testModel(), WithTools(echoTool()), WithMiddleware(m1, m2))
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.False(t, m2Called, "second middleware should not run when first blocks")
}

// Test 8: Chain of one — single middleware works like unwrapped.
func TestMiddleware_ChainOfOne(t *testing.T) {
	var called bool
	mw := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		called = true
		return next(ctx)
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(testModel(), WithTools(echoTool()), WithMiddleware(mw))
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.True(t, called)
	toolResult := findToolResult(t, msgs)
	assert.False(t, toolResult.IsError)
}

// Test 9: Parallel tools — middleware called for both concurrently.
func TestMiddleware_ParallelTools(t *testing.T) {
	var count atomic.Int32
	mw := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		count.Add(1)
		return next(ctx)
	}

	calls := []ai.ToolCall{
		{ID: "call_1", Name: "par_a", Arguments: map[string]any{"input": "x"}},
		{ID: "call_2", Name: "par_b", Arguments: map[string]any{"input": "y"}},
	}
	registerMock(t,
		toolCallStream(calls, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(
		testModel(),
		WithTools(parallelEchoTool("par_a"), parallelEchoTool("par_b")),
		WithMiddleware(mw),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, int32(2), count.Load(), "middleware should be called for both parallel tools")
}

// Test 10: No middleware — nil middleware, identical behavior.
func TestMiddleware_NilMiddleware(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "hello"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(testModel(), WithTools(echoTool()))
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	toolResult := findToolResult(t, msgs)
	assert.False(t, toolResult.IsError)
	text, ok := ai.AsContent[ai.Text](toolResult.Content[0])
	require.True(t, ok)
	assert.Equal(t, "hello", text.Text)
}

// --- TransformMessages hook tests ---

// Test: TransformMessages replaces LLMMessages conversion.
func TestTransformMessages_ReplacesDefault(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	var received []Message
	transform := func(_ context.Context, msgs []Message) []ai.Message {
		received = msgs
		return LLMMessages(msgs)
	}

	a := New(testModel(), WithHooks(Hooks{
		TransformMessages: transform,
	}))
	_, err := a.Send(t.Context(), "hello").Result()
	require.NoError(t, err)

	// Transform should have received the agent messages (user message).
	require.Len(t, received, 1)
	assert.Equal(t, RoleUser, received[0].Role())

	// Provider should still get the correct messages.
	require.Len(t, mock.prompts, 1)
	require.Len(t, mock.prompts[0].Messages, 1)
	assert.Equal(t, ai.RoleUser, mock.prompts[0].Messages[0].Role)
}

// Test: TransformMessages can filter messages.
func TestTransformMessages_FiltersMessages(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	// Drop all messages — send empty slice to provider.
	transform := func(_ context.Context, _ []Message) []ai.Message {
		return nil
	}

	history := []Message{
		NewLLMMessage(ai.UserMessage("old")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "old reply"})),
	}
	a := New(
		testModel(),
		WithHistory(history...),
		WithHooks(Hooks{TransformMessages: transform}),
	)
	_, err := a.Send(t.Context(), "new").Result()
	require.NoError(t, err)

	// Provider should have received zero messages.
	require.Len(t, mock.prompts, 1)
	assert.Empty(t, mock.prompts[0].Messages)
}

// Test: TransformMessages receives custom messages.
func TestTransformMessages_ReceivesCustomMessages(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	type artifact struct {
		CustomMessage
		Content string
	}

	var sawCustom bool
	transform := func(_ context.Context, msgs []Message) []ai.Message {
		for _, m := range msgs {
			if _, ok := m.(artifact); ok {
				sawCustom = true
			}
		}
		return LLMMessages(msgs)
	}

	history := []Message{
		artifact{
			CustomMessage: CustomMessage{CustomRole: RoleAssistant, Kind: "artifact"},
			Content:       "code snippet",
		},
	}
	a := New(
		testModel(),
		WithHistory(history...),
		WithHooks(Hooks{TransformMessages: transform}),
	)
	_, err := a.Send(t.Context(), "hi").Result()
	require.NoError(t, err)

	assert.True(t, sawCustom, "transform should see custom messages")
	// Custom messages should be filtered out by LLMMessages, so provider
	// should only see the user message.
	require.Len(t, mock.prompts, 1)
	require.Len(t, mock.prompts[0].Messages, 1)
	assert.Equal(t, ai.RoleUser, mock.prompts[0].Messages[0].Role)
}

// Test: TransformMessages called each turn (multi-turn).
func TestTransformMessages_CalledEachTurn(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	var callCount int
	transform := func(_ context.Context, msgs []Message) []ai.Message {
		callCount++
		return LLMMessages(msgs)
	}

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHooks(Hooks{TransformMessages: transform}),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, 2, callCount, "transform should be called once per turn")
}

// Test: Nil TransformMessages falls back to LLMMessages.
func TestTransformMessages_NilFallback(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	a := New(testModel(), WithHooks(Hooks{}))
	_, err := a.Send(t.Context(), "hi").Result()
	require.NoError(t, err)

	require.Len(t, mock.prompts, 1)
	require.Len(t, mock.prompts[0].Messages, 1)
}

// --- AfterTurn hook tests ---

// Test: AfterTurn receives messages and turn result after each turn.
func TestAfterTurn_CalledWithState(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{Input: 10}),
		textStream("done", ai.Usage{Input: 20}),
	)

	var turns []TurnResult
	afterTurn := func(_ context.Context, _ []Message, tr TurnResult) []Message {
		turns = append(turns, tr)
		return nil
	}

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHooks(Hooks{AfterTurn: afterTurn}),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	require.Len(t, turns, 2)

	// Turn 1: tool call, has tool results, continues.
	assert.Equal(t, ai.StopReasonToolUse, turns[0].AssistantMsg.StopReason)
	assert.Len(t, turns[0].ToolResults, 1)

	// Turn 2: text response, no tool results, stops.
	assert.Equal(t, ai.StopReasonStop, turns[1].AssistantMsg.StopReason)
	assert.Empty(t, turns[1].ToolResults)
}

// Test: AfterTurn can replace messages (compaction).
func TestAfterTurn_ReplacesMessages(t *testing.T) {
	call1 := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	call2 := ai.ToolCall{
		ID:        "call_2",
		Name:      "echo",
		Arguments: map[string]any{"input": "y"},
	}
	mock := registerMock(t,
		toolCallStream([]ai.ToolCall{call1}, ai.Usage{}),
		toolCallStream([]ai.ToolCall{call2}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	afterTurn := func(_ context.Context, msgs []Message, _ TurnResult) []Message {
		// After turn 1: [user, assistant, tool_result] = 3, no compaction.
		// After turn 2: [user, assistant, tool_result, assistant, tool_result] = 5, compact.
		if len(msgs) >= 5 {
			return msgs[len(msgs)-2:]
		}
		return nil
	}

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHooks(Hooks{AfterTurn: afterTurn}),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	// Turn 3 buildPrompt should see the compacted 2 messages.
	require.Len(t, mock.prompts, 3)
	assert.Len(t, mock.prompts[2].Messages, 2, "third turn should see compacted messages")
}

// Test: AfterTurn nil means no change.
func TestAfterTurn_NilNoChange(t *testing.T) {
	registerMock(t, textStream("ok", ai.Usage{}))

	a := New(testModel(), WithHooks(Hooks{}))
	_, err := a.Send(t.Context(), "hi").Result()
	require.NoError(t, err)

	assert.Len(t, a.Messages(), 2) // user + assistant
}

// --- FollowUp hook tests ---

// Test: FollowUp injects messages to continue the loop.
func TestFollowUp_ContinuesLoop(t *testing.T) {
	registerMock(t,
		textStream("first answer", ai.Usage{}),
		textStream("second answer", ai.Usage{}),
	)

	calls := 0
	followUp := func(_ context.Context, _ []Message) []Message {
		calls++
		if calls == 1 {
			return []Message{
				NewLLMMessage(ai.UserMessage("now verify that")),
			}
		}
		return nil // stop on second call
	}

	a := New(testModel(), WithHooks(Hooks{FollowUp: followUp}))
	msgs, err := a.Send(t.Context(), "do something").Result()
	require.NoError(t, err)

	// first answer + injected user msg + second answer
	require.Len(t, msgs, 3)
	assert.Equal(t, ai.RoleAssistant, msgs[0].Role)
	assert.Equal(t, ai.RoleUser, msgs[1].Role)
	assert.Equal(t, ai.RoleAssistant, msgs[2].Role)
}

// Test: FollowUp returning nil lets agent stop.
func TestFollowUp_NilStops(t *testing.T) {
	registerMock(t, textStream("done", ai.Usage{}))

	followUp := func(_ context.Context, _ []Message) []Message {
		return nil
	}

	a := New(testModel(), WithHooks(Hooks{FollowUp: followUp}))
	msgs, err := a.Send(t.Context(), "hi").Result()
	require.NoError(t, err)

	require.Len(t, msgs, 1)
}

// Test: FollowUp respects maxTurns.
func TestFollowUp_RespectsMaxTurns(t *testing.T) {
	registerMock(t,
		textStream("turn 1", ai.Usage{}),
		textStream("turn 2", ai.Usage{}),
		textStream("unreachable", ai.Usage{}),
	)

	followUp := func(_ context.Context, _ []Message) []Message {
		return []Message{NewLLMMessage(ai.UserMessage("continue"))}
	}

	a := New(
		testModel(),
		WithMaxTurns(2),
		WithHooks(Hooks{FollowUp: followUp}),
	)
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	// 2 turns max: assistant + injected user + assistant
	require.Len(t, msgs, 3)
}

// Test: FollowUp not called when tool calls continue the loop.
func TestFollowUp_NotCalledOnToolContinue(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	var called int
	followUp := func(_ context.Context, _ []Message) []Message {
		called++
		return nil
	}

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHooks(Hooks{FollowUp: followUp}),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	// Only called once — after the final text response, not after tool turn.
	assert.Equal(t, 1, called)
}

// Test: FollowUp + AfterTurn + TransformMessages all work together.
func TestHooks_AllTogether(t *testing.T) {
	mock := registerMock(t,
		textStream("first", ai.Usage{}),
		textStream("second", ai.Usage{}),
	)

	var transformCalls int
	var afterTurnCalls int

	a := New(testModel(), WithHooks(Hooks{
		TransformMessages: func(_ context.Context, msgs []Message) []ai.Message {
			transformCalls++
			return LLMMessages(msgs)
		},
		AfterTurn: func(_ context.Context, _ []Message, _ TurnResult) []Message {
			afterTurnCalls++
			return nil
		},
		FollowUp: func(_ context.Context, _ []Message) []Message {
			if afterTurnCalls == 1 {
				return []Message{NewLLMMessage(ai.UserMessage("more"))}
			}
			return nil
		},
	}))
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, 2, transformCalls, "transform called each turn")
	assert.Equal(t, 2, afterTurnCalls, "afterTurn called each turn")
	require.Len(t, msgs, 3) // first + injected + second
	require.Len(t, mock.prompts, 2)
}

// Test 11: Full scenario — middleware + multi-turn tool calls.
func TestMiddleware_FullScenario(t *testing.T) {
	var callNames []string
	mw := func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		callNames = append(callNames, call.Name)
		return next(ctx)
	}

	call1 := ai.ToolCall{ID: "call_1", Name: "echo", Arguments: map[string]any{"input": "a"}}
	call2 := ai.ToolCall{ID: "call_2", Name: "echo", Arguments: map[string]any{"input": "b"}}
	registerMock(t,
		toolCallStream([]ai.ToolCall{call1}, ai.Usage{}),
		toolCallStream([]ai.ToolCall{call2}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(testModel(), WithTools(echoTool()), WithMiddleware(mw))
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, []string{"echo", "echo"}, callNames)
}
