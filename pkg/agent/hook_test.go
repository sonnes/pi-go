package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- BeforeTool hook tests ---

// Test: BeforeTool passes through — tool runs normally.
func TestBeforeTool_PassesThrough(t *testing.T) {
	var called bool
	hook := func(
		_ context.Context,
		input *HookInput,
	) (*HookOutput, error) {
		called = true
		assert.Equal(t, HookBeforeTool, input.Event)
		assert.Equal(t, "echo", input.ToolCall.Name)
		return nil, nil
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

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookBeforeTool, hook),
	)
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.True(t, called, "hook should have been called")

	toolResult := findToolResult(t, msgs)
	assert.False(t, toolResult.IsError)
}

// Test: BeforeTool blocks execution — returns Deny.
func TestBeforeTool_BlocksExecution(t *testing.T) {
	var toolRan bool
	guardedTool := ai.DefineTool[toolInput, string](
		"guarded",
		"guarded tool",
		func(_ context.Context, in toolInput) (string, error) {
			toolRan = true
			return in.Input, nil
		},
	)

	hook := func(
		_ context.Context,
		_ *HookInput,
	) (*HookOutput, error) {
		return &HookOutput{Deny: true, DenyReason: "blocked by policy"}, nil
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

	a := New(
		testModel(),
		WithTools(guardedTool),
		WithHook(HookBeforeTool, hook),
	)
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.False(t, toolRan, "tool should not have been called")

	toolResult := findToolResult(t, msgs)
	assert.True(t, toolResult.IsError)
	assert.Contains(t, toolResult.Content[0].(ai.Text).Text, "blocked by policy")
}

// Test: BeforeTool returns error.
func TestBeforeTool_ReturnsError(t *testing.T) {
	hook := func(
		_ context.Context,
		_ *HookInput,
	) (*HookOutput, error) {
		return nil, assert.AnError
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

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookBeforeTool, hook),
	)
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	toolResult := findToolResult(t, msgs)
	assert.True(t, toolResult.IsError)
}

// Test: Multiple BeforeTool hooks — first deny short-circuits.
func TestBeforeTool_FirstDenyWins(t *testing.T) {
	var h2Called bool

	h1 := func(_ context.Context, _ *HookInput) (*HookOutput, error) {
		return &HookOutput{Deny: true, DenyReason: "blocked"}, nil
	}
	h2 := func(_ context.Context, _ *HookInput) (*HookOutput, error) {
		h2Called = true
		return nil, nil
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

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookBeforeTool, h1),
		WithHook(HookBeforeTool, h2),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.False(t, h2Called, "second hook should not run when first denies")
}

// Test: BeforeTool with parallel tools — hook called for each.
func TestBeforeTool_ParallelTools(t *testing.T) {
	var count atomic.Int32
	hook := func(_ context.Context, _ *HookInput) (*HookOutput, error) {
		count.Add(1)
		return nil, nil
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
		WithHook(HookBeforeTool, hook),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, int32(2), count.Load(), "hook should be called for both parallel tools")
}

// Test: No hooks — nil hooks, identical behavior.
func TestHook_NilHooks(t *testing.T) {
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

// --- AfterTool hook tests ---

// Test: AfterTool modifies result.
func TestAfterTool_ModifiesResult(t *testing.T) {
	hook := func(
		_ context.Context,
		input *HookInput,
	) (*HookOutput, error) {
		assert.Equal(t, HookAfterTool, input.Event)
		assert.NotNil(t, input.ToolResult)
		modified := *input.ToolResult
		modified.Content = "modified"
		return &HookOutput{ToolResult: &modified}, nil
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

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookAfterTool, hook),
	)
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	toolResult := findToolResult(t, msgs)
	assert.False(t, toolResult.IsError)
	text, ok := ai.AsContent[ai.Text](toolResult.Content[0])
	require.True(t, ok)
	assert.Equal(t, "modified", text.Text)
}

// Test: Multiple AfterTool hooks chain — each sees previous result.
func TestAfterTool_Chains(t *testing.T) {
	h1 := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		modified := *input.ToolResult
		modified.Content = modified.Content + "+h1"
		return &HookOutput{ToolResult: &modified}, nil
	}
	h2 := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		modified := *input.ToolResult
		modified.Content = modified.Content + "+h2"
		return &HookOutput{ToolResult: &modified}, nil
	}

	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "base"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookAfterTool, h1),
		WithHook(HookAfterTool, h2),
	)
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	toolResult := findToolResult(t, msgs)
	text, ok := ai.AsContent[ai.Text](toolResult.Content[0])
	require.True(t, ok)
	assert.Equal(t, "base+h1+h2", text.Text)
}

// --- BeforeCall hook tests ---

// Test: BeforeCall replaces LLM messages.
func TestBeforeCall_ReplacesLLMMessages(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	var received []Message
	hook := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		received = input.Messages
		return &HookOutput{LLMMessages: LLMMessages(input.Messages)}, nil
	}

	a := New(testModel(), WithHook(HookBeforeCall, hook))
	_, err := a.Send(t.Context(), "hello").Result()
	require.NoError(t, err)

	require.Len(t, received, 1)
	assert.Equal(t, RoleUser, received[0].Role())

	require.Len(t, mock.prompts, 1)
	require.Len(t, mock.prompts[0].Messages, 1)
	assert.Equal(t, ai.RoleUser, mock.prompts[0].Messages[0].Role)
}

// Test: BeforeCall can filter messages.
func TestBeforeCall_FiltersMessages(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	// Return an empty slice (not nil) to explicitly send zero messages.
	hook := func(_ context.Context, _ *HookInput) (*HookOutput, error) {
		return &HookOutput{LLMMessages: []ai.Message{}}, nil
	}

	history := []Message{
		NewLLMMessage(ai.UserMessage("old")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "old reply"})),
	}
	a := New(
		testModel(),
		WithHistory(history...),
		WithHook(HookBeforeCall, hook),
	)
	_, err := a.Send(t.Context(), "new").Result()
	require.NoError(t, err)

	require.Len(t, mock.prompts, 1)
	assert.Empty(t, mock.prompts[0].Messages)
}

// Test: BeforeCall receives custom messages.
func TestBeforeCall_ReceivesCustomMessages(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	type artifact struct {
		CustomMessage
		Content string
	}

	var sawCustom bool
	hook := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		for _, m := range input.Messages {
			if _, ok := m.(artifact); ok {
				sawCustom = true
			}
		}
		return &HookOutput{LLMMessages: LLMMessages(input.Messages)}, nil
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
		WithHook(HookBeforeCall, hook),
	)
	_, err := a.Send(t.Context(), "hi").Result()
	require.NoError(t, err)

	assert.True(t, sawCustom, "hook should see custom messages")
	require.Len(t, mock.prompts, 1)
	require.Len(t, mock.prompts[0].Messages, 1)
	assert.Equal(t, ai.RoleUser, mock.prompts[0].Messages[0].Role)
}

// Test: BeforeCall called each turn (multi-turn).
func TestBeforeCall_CalledEachTurn(t *testing.T) {
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
	hook := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		callCount++
		return &HookOutput{LLMMessages: LLMMessages(input.Messages)}, nil
	}

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookBeforeCall, hook),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, 2, callCount, "hook should be called once per turn")
}

// Test: Nil BeforeCall falls back to LLMMessages.
func TestBeforeCall_NilFallback(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	a := New(testModel())
	_, err := a.Send(t.Context(), "hi").Result()
	require.NoError(t, err)

	require.Len(t, mock.prompts, 1)
	require.Len(t, mock.prompts[0].Messages, 1)
}

// Test: Multiple BeforeCall hooks chain — Messages field chains.
func TestBeforeCall_ChainsMessages(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	// First hook filters agent messages (keep only last one).
	h1 := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		if len(input.Messages) > 1 {
			return &HookOutput{Messages: input.Messages[len(input.Messages)-1:]}, nil
		}
		return nil, nil
	}
	// Second hook converts to LLM messages.
	h2 := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		return &HookOutput{LLMMessages: LLMMessages(input.Messages)}, nil
	}

	history := []Message{
		NewLLMMessage(ai.UserMessage("old")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "old reply"})),
	}
	a := New(
		testModel(),
		WithHistory(history...),
		WithHook(HookBeforeCall, h1),
		WithHook(HookBeforeCall, h2),
	)
	_, err := a.Send(t.Context(), "new").Result()
	require.NoError(t, err)

	// h1 kept only "new" message, h2 converted it.
	require.Len(t, mock.prompts, 1)
	require.Len(t, mock.prompts[0].Messages, 1)
	assert.Equal(t, ai.RoleUser, mock.prompts[0].Messages[0].Role)
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
	hook := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		assert.Equal(t, HookAfterTurn, input.Event)
		assert.NotNil(t, input.Turn)
		turns = append(turns, *input.Turn)
		return nil, nil
	}

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookAfterTurn, hook),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	require.Len(t, turns, 2)

	assert.Equal(t, ai.StopReasonToolUse, turns[0].AssistantMsg.StopReason)
	assert.Len(t, turns[0].ToolResults, 1)

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

	hook := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		if len(input.Messages) >= 5 {
			return &HookOutput{Messages: input.Messages[len(input.Messages)-2:]}, nil
		}
		return nil, nil
	}

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookAfterTurn, hook),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	require.Len(t, mock.prompts, 3)
	assert.Len(t, mock.prompts[2].Messages, 2, "third turn should see compacted messages")
}

// Test: AfterTurn nil output means no change.
func TestAfterTurn_NilNoChange(t *testing.T) {
	registerMock(t, textStream("ok", ai.Usage{}))

	a := New(testModel())
	_, err := a.Send(t.Context(), "hi").Result()
	require.NoError(t, err)

	assert.Len(t, a.Messages(), 2) // user + assistant
}

// --- BeforeStop hook tests ---

// Test: BeforeStop injects messages to continue the loop.
func TestBeforeStop_ContinuesLoop(t *testing.T) {
	registerMock(t,
		textStream("first answer", ai.Usage{}),
		textStream("second answer", ai.Usage{}),
	)

	calls := 0
	hook := func(_ context.Context, _ *HookInput) (*HookOutput, error) {
		calls++
		if calls == 1 {
			return &HookOutput{
				FollowUp: []Message{
					NewLLMMessage(ai.UserMessage("now verify that")),
				},
			}, nil
		}
		return nil, nil
	}

	a := New(testModel(), WithHook(HookBeforeStop, hook))
	msgs, err := a.Send(t.Context(), "do something").Result()
	require.NoError(t, err)

	require.Len(t, msgs, 3)
	assert.Equal(t, ai.RoleAssistant, msgs[0].Role)
	assert.Equal(t, ai.RoleUser, msgs[1].Role)
	assert.Equal(t, ai.RoleAssistant, msgs[2].Role)
}

// Test: BeforeStop returning nil lets agent stop.
func TestBeforeStop_NilStops(t *testing.T) {
	registerMock(t, textStream("done", ai.Usage{}))

	hook := func(_ context.Context, _ *HookInput) (*HookOutput, error) {
		return nil, nil
	}

	a := New(testModel(), WithHook(HookBeforeStop, hook))
	msgs, err := a.Send(t.Context(), "hi").Result()
	require.NoError(t, err)

	require.Len(t, msgs, 1)
}

// Test: BeforeStop respects maxTurns.
func TestBeforeStop_RespectsMaxTurns(t *testing.T) {
	registerMock(t,
		textStream("turn 1", ai.Usage{}),
		textStream("turn 2", ai.Usage{}),
		textStream("unreachable", ai.Usage{}),
	)

	hook := func(_ context.Context, _ *HookInput) (*HookOutput, error) {
		return &HookOutput{
			FollowUp: []Message{NewLLMMessage(ai.UserMessage("continue"))},
		}, nil
	}

	a := New(
		testModel(),
		WithMaxTurns(2),
		WithHook(HookBeforeStop, hook),
	)
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	require.Len(t, msgs, 3)
}

// Test: BeforeStop not called when tool calls continue the loop.
func TestBeforeStop_NotCalledOnToolContinue(t *testing.T) {
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
	hook := func(_ context.Context, _ *HookInput) (*HookOutput, error) {
		called++
		return nil, nil
	}

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookBeforeStop, hook),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, 1, called)
}

// --- Combined hook tests ---

// Test: All hooks work together.
func TestHooks_AllTogether(t *testing.T) {
	mock := registerMock(t,
		textStream("first", ai.Usage{}),
		textStream("second", ai.Usage{}),
	)

	var beforeCallCalls int
	var afterTurnCalls int

	a := New(testModel(),
		WithHook(HookBeforeCall, func(_ context.Context, input *HookInput) (*HookOutput, error) {
			beforeCallCalls++
			return &HookOutput{LLMMessages: LLMMessages(input.Messages)}, nil
		}),
		WithHook(HookAfterTurn, func(_ context.Context, _ *HookInput) (*HookOutput, error) {
			afterTurnCalls++
			return nil, nil
		}),
		WithHook(HookBeforeStop, func(_ context.Context, _ *HookInput) (*HookOutput, error) {
			if afterTurnCalls == 1 {
				return &HookOutput{
					FollowUp: []Message{NewLLMMessage(ai.UserMessage("more"))},
				}, nil
			}
			return nil, nil
		}),
	)
	msgs, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, 2, beforeCallCalls, "beforeCall called each turn")
	assert.Equal(t, 2, afterTurnCalls, "afterTurn called each turn")
	require.Len(t, msgs, 3)
	require.Len(t, mock.prompts, 2)
}

// Test: Full scenario — BeforeTool + AfterTool + multi-turn tool calls.
func TestHooks_FullScenario(t *testing.T) {
	var callNames []string
	beforeHook := func(_ context.Context, input *HookInput) (*HookOutput, error) {
		callNames = append(callNames, input.ToolCall.Name)
		return nil, nil
	}

	call1 := ai.ToolCall{ID: "call_1", Name: "echo", Arguments: map[string]any{"input": "a"}}
	call2 := ai.ToolCall{ID: "call_2", Name: "echo", Arguments: map[string]any{"input": "b"}}
	registerMock(t,
		toolCallStream([]ai.ToolCall{call1}, ai.Usage{}),
		toolCallStream([]ai.ToolCall{call2}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(
		testModel(),
		WithTools(echoTool()),
		WithHook(HookBeforeTool, beforeHook),
	)
	_, err := a.Send(t.Context(), "go").Result()
	require.NoError(t, err)

	assert.Equal(t, []string{"echo", "echo"}, callNames)
}

// Test: Context flows through BeforeTool to tool execution.
func TestBeforeTool_ContextFlows(t *testing.T) {
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

	// This test verifies the hook receives the correct context.
	var hookCtxOK bool
	hook := func(ctx context.Context, _ *HookInput) (*HookOutput, error) {
		hookCtxOK = ctx != nil
		return nil, nil
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

	ctx := context.WithValue(t.Context(), ctxKey{}, "from-test")
	a := New(testModel(), WithTools(ctxTool), WithHook(HookBeforeTool, hook))
	_, err := a.Send(ctx, "go").Result()
	require.NoError(t, err)

	assert.True(t, hookCtxOK)
	assert.Equal(t, "from-test", toolSawValue)
}
