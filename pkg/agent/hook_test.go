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
