package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock provider infrastructure ---

const mockAPI = "mock-test"

// mockProvider implements [ai.Provider] with a scripted sequence of responses.
type mockProvider struct {
	mu        sync.Mutex
	responses []*ai.EventStream
	callIdx   int
	prompts   []ai.Prompt
}

func (m *mockProvider) API() string { return mockAPI }

func (m *mockProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	p ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.prompts = append(m.prompts, p)

	if m.callIdx >= len(m.responses) {
		return ai.NewEventStream(func(push func(ai.Event)) {
			push(ai.Event{
				Type: ai.EventError,
				Err:  fmt.Errorf("mock: no more responses (call %d)", m.callIdx),
			})
		})
	}

	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp
}

func testModel() ai.Model {
	return ai.Model{ID: "test-model", API: mockAPI}
}

func registerMock(t *testing.T, responses ...*ai.EventStream) *mockProvider {
	t.Helper()
	m := &mockProvider{responses: responses}
	ai.RegisterProvider(mockAPI, m)
	t.Cleanup(ai.ClearProviders)
	return m
}

// textStream creates an [ai.EventStream] that streams a text response
// using realistic provider semantics: Message is only set on EventDone.
func textStream(text string, usage ai.Usage) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) {
		msg := &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    []ai.Content{ai.Text{Text: text}},
			StopReason: ai.StopReasonStop,
			Usage:      usage,
		}
		push(ai.Event{Type: ai.EventTextStart, ContentIndex: 0})
		push(ai.Event{Type: ai.EventTextDelta, ContentIndex: 0, Delta: text})
		push(ai.Event{Type: ai.EventTextEnd, ContentIndex: 0, Content: text})
		push(ai.Event{Type: ai.EventDone, Message: msg, StopReason: ai.StopReasonStop})
	})
}

// toolCallStream creates an [ai.EventStream] that returns tool call(s)
// using realistic provider semantics: Message is only set on EventDone.
func toolCallStream(calls []ai.ToolCall, usage ai.Usage) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) {
		content := make([]ai.Content, len(calls))
		for i, tc := range calls {
			content[i] = tc
		}
		msg := &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    content,
			StopReason: ai.StopReasonToolUse,
			Usage:      usage,
		}
		for i, tc := range calls {
			push(ai.Event{Type: ai.EventToolStart, ContentIndex: i})
			push(ai.Event{Type: ai.EventToolEnd, ContentIndex: i, ToolCall: &tc})
		}
		push(ai.Event{Type: ai.EventDone, Message: msg, StopReason: ai.StopReasonToolUse})
	})
}

// errorStream creates an [ai.EventStream] that immediately errors.
func errorStream(err error) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) {
		push(ai.Event{
			Type: ai.EventError,
			Err:  err,
		})
	})
}

// blockingStream creates an [ai.EventStream] that blocks until the context is canceled.
func blockingStream(ctx context.Context) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) {
		<-ctx.Done()
		push(ai.Event{
			Type: ai.EventError,
			Err:  ctx.Err(),
		})
	})
}

// collectEvents subscribes to the agent, runs fn, and collects events until EventAgentEnd.
func collectEvents(t *testing.T, a *Default, fn func()) []Event {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := a.Subscribe(ctx)
	fn()
	var events []Event
	for pe := range ch {
		evt := pe.Payload()
		events = append(events, evt)
		if evt.Type == EventAgentEnd {
			break
		}
	}
	return events
}

// sendAndWait sends input to the agent and blocks until done.
func sendAndWait(t *testing.T, a *Default, input string) ([]ai.Message, error) {
	t.Helper()
	err := a.Send(t.Context(), input)
	require.NoError(t, err)
	return a.Wait(t.Context())
}

// findToolResult finds the first tool result message in a slice.
func findToolResult(t *testing.T, msgs []ai.Message) *ai.Message {
	t.Helper()
	for i := range msgs {
		if msgs[i].Role == ai.RoleToolResult {
			return &msgs[i]
		}
	}
	t.Fatal("no tool result message found")
	return nil
}

// eventTypes extracts event types from a slice of events.
func eventTypes(events []Event) []EventType {
	types := make([]EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

// toolInput is the common input shape for test tools, matching
// ToolCall.Arguments of {"input": "..."}.
type toolInput struct {
	Input string `json:"input"`
}

// echoTool creates a tool that returns its input as-is.
func echoTool() ai.Tool {
	return ai.DefineTool[toolInput, string](
		"echo",
		"echoes input",
		func(_ context.Context, in toolInput) (string, error) {
			return in.Input, nil
		},
	)
}

// errorTool creates a tool that always returns an error.
func errorTool() ai.Tool {
	return ai.DefineTool[toolInput, string](
		"fail",
		"always fails",
		func(_ context.Context, _ toolInput) (string, error) {
			return "", errors.New("tool error")
		},
	)
}

// panicTool creates a tool that panics.
func panicTool() ai.Tool {
	return ai.DefineTool[toolInput, string](
		"panic_tool",
		"panics",
		func(_ context.Context, _ toolInput) (string, error) {
			panic("tool panic")
		},
	)
}

// parallelEchoTool creates a parallel-safe echo tool with a given name.
func parallelEchoTool(name string) ai.Tool {
	return ai.DefineParallelTool[toolInput, string](
		name,
		"parallel echo",
		func(_ context.Context, in toolInput) (string, error) {
			return in.Input, nil
		},
	)
}

// blockingTool creates a tool that blocks until the context is canceled.
func blockingTool(name string) ai.Tool {
	return ai.DefineTool[toolInput, string](
		name,
		"blocks until canceled",
		func(ctx context.Context, _ toolInput) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	)
}

// --- Existing constructor tests ---

func TestWithProvider_BypassesGlobalRegistry(t *testing.T) {
	// Do not call registerMock — the global registry must stay empty so
	// the test proves routing went through WithProvider instead.
	t.Cleanup(ai.ClearProviders)

	m := &mockProvider{responses: []*ai.EventStream{textStream("hi", ai.Usage{})}}
	a := New(
		WithModel(ai.Model{ID: "test-model"}), // no API set — provider is bound directly
		WithProvider(m),
	)

	msgs, err := sendAndWait(t, a, "hello")
	require.NoError(t, err)
	require.NotEmpty(t, msgs)

	assert.Len(t, m.prompts, 1, "bound provider must receive the call")
}

func TestRun_NoModelOrProvider(t *testing.T) {
	a := New()
	err := a.Send(t.Context(), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no model configured")
}

func TestNewDefault_Defaults(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	a := New(WithModel(model))

	assert.False(t, a.IsRunning())
	assert.NoError(t, a.Err())
	assert.Empty(t, a.Messages())
	assert.Equal(t, model, a.config.model)
	assert.Empty(t, a.config.tools)
	assert.Equal(t, 0, a.config.maxTurns)
}

func TestNewDefault_WithTools(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	tool := ai.DefineTool[string, string](
		"echo",
		"echoes input",
		func(_ context.Context, in string) (string, error) {
			return in, nil
		},
	)

	a := New(WithModel(model), WithTools(tool))

	assert.Len(t, a.config.tools, 1)
}

func TestNewDefault_WithServerTool_NotInToolMap(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	srv := ai.DefineServerTool(ai.ToolInfo{
		ServerType: ai.ServerToolWebSearch,
	})

	a := New(WithModel(model), WithTools(srv))

	// ToolInfo is advertised to the model so it shows up in c.tools and
	// in the toolInfo slice.
	assert.Len(t, a.config.tools, 1)
	assert.Len(t, a.toolInfo, 1)
	assert.Equal(t, ai.ToolKindServer, a.toolInfo[0].Kind)

	// But it must NOT be in toolMap — the agent never executes server
	// tools locally.
	_, found := a.toolMap["web_search"]
	assert.False(t, found, "server tool must not be registered for local execution")
}

func TestFilterFunctionCalls(t *testing.T) {
	calls := []ai.ToolCall{
		{ID: "1", Name: "echo"},
		{ID: "2", Name: "web_search", Server: true, ServerType: ai.ServerToolWebSearch},
		{ID: "3", Name: "lookup"},
		{ID: "4", Name: "code_execution", Server: true, ServerType: ai.ServerToolCodeExecution},
	}

	got := filterFunctionCalls(calls)

	require.Len(t, got, 2)
	assert.Equal(t, "echo", got[0].Name)
	assert.Equal(t, "lookup", got[1].Name)
}

func TestNewDefault_WithHistory(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	msgs := []Message{
		NewLLMMessage(ai.UserMessage("hello")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "hi"})),
	}

	a := New(WithModel(model), WithHistory(msgs...))

	assert.Equal(t, msgs, a.Messages())
}

func TestNewDefault_WithHistory_IsCopied(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	msgs := []Message{NewLLMMessage(ai.UserMessage("hello"))}

	a := New(WithModel(model), WithHistory(msgs...))

	// Mutate original — should not affect agent state.
	msgs[0] = NewLLMMessage(ai.UserMessage("modified"))

	got := a.Messages()
	lm, ok := AsLLMMessage(got[0])
	assert.True(t, ok)
	assert.Equal(t, "hello", lm.Content[0].(ai.Text).Text)
}

func TestNewDefault_WithMaxTurns(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	a := New(WithModel(model), WithMaxTurns(5))

	assert.Equal(t, 5, a.config.maxTurns)
}

func TestNewDefault_WithSystemPrompt(t *testing.T) {
	model := ai.Model{ID: "test-model"}

	a := New(WithModel(model), WithSystemPrompt("be helpful"))

	assert.Equal(t, "be helpful", a.config.systemPrompt)
}

func TestNewDefault_WithStreamOpts(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	opts := []ai.Option{ai.WithMaxTokens(100)}

	a := New(WithModel(model), WithStreamOpts(opts...))

	assert.Len(t, a.config.streamOpts, 1)
}

func TestNewDefault_MultipleOptions(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	tool := ai.DefineTool[string, string](
		"echo",
		"echoes input",
		func(_ context.Context, in string) (string, error) {
			return in, nil
		},
	)
	msgs := []Message{NewLLMMessage(ai.UserMessage("hello"))}

	a := New(
		WithModel(model),
		WithTools(tool),
		WithHistory(msgs...),
		WithMaxTurns(10),
	)

	assert.Len(t, a.config.tools, 1)
	assert.Len(t, a.Messages(), 1)
	assert.Equal(t, 10, a.config.maxTurns)
}

// --- Agent loop tests ---

// Test 1: Simple text response — no tools, verify events + Result()
func TestSend_SimpleTextResponse(t *testing.T) {
	usage := ai.Usage{Input: 10, Output: 20, Total: 30}
	registerMock(t, textStream("Hello!", usage))

	a := New(WithModel(testModel()))
	msgs, err := sendAndWait(t, a, "hi")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, ai.RoleAssistant, msgs[0].Role)
	assert.Equal(t, ai.StopReasonStop, msgs[0].StopReason)

	assert.False(t, a.IsRunning())
	assert.NoError(t, a.Err())
	assert.Len(t, a.Messages(), 2)
}

// Test 2: Single tool call → result → final response
func TestSend_SingleToolCall(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "test"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("Done!", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(echoTool()))
	msgs, err := sendAndWait(t, a, "call echo")
	require.NoError(t, err)
	// assistant (tool call) + tool result + assistant (final)
	require.Len(t, msgs, 3)
	assert.Equal(t, ai.StopReasonToolUse, msgs[0].StopReason)
	assert.Equal(t, ai.RoleToolResult, msgs[1].Role)
	assert.Equal(t, ai.StopReasonStop, msgs[2].StopReason)
}

// Test 3: Multi-turn tool calls (2+ rounds)
func TestSend_MultiTurnToolCalls(t *testing.T) {
	call1 := ai.ToolCall{ID: "call_1", Name: "echo", Arguments: map[string]any{"input": "a"}}
	call2 := ai.ToolCall{ID: "call_2", Name: "echo", Arguments: map[string]any{"input": "b"}}
	registerMock(t,
		toolCallStream([]ai.ToolCall{call1}, ai.Usage{}),
		toolCallStream([]ai.ToolCall{call2}, ai.Usage{}),
		textStream("All done", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(echoTool()))
	msgs, err := sendAndWait(t, a, "do two things")
	require.NoError(t, err)
	// turn 1: assistant (tool call) + tool result
	// turn 2: assistant (tool call) + tool result
	// turn 3: assistant (final)
	require.Len(t, msgs, 5)
}

// Test 4: Parallel tool execution (all parallel-safe)
func TestSend_ParallelToolExecution(t *testing.T) {
	calls := []ai.ToolCall{
		{ID: "call_1", Name: "par_a", Arguments: map[string]any{"input": "x"}},
		{ID: "call_2", Name: "par_b", Arguments: map[string]any{"input": "y"}},
	}
	registerMock(t,
		toolCallStream(calls, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(
		WithModel(testModel()),
		WithTools(parallelEchoTool("par_a"), parallelEchoTool("par_b")),
	)
	msgs, err := sendAndWait(t, a, "parallel")
	require.NoError(t, err)
	// assistant (tool calls) + 2 tool results + assistant (final)
	require.Len(t, msgs, 4)

	// Both tool results should be present (order may vary in parallel).
	toolResultIDs := map[string]bool{}
	for _, m := range msgs {
		if m.Role == ai.RoleToolResult {
			toolResultIDs[m.ToolCallID] = true
		}
	}
	assert.True(t, toolResultIDs["call_1"])
	assert.True(t, toolResultIDs["call_2"])
}

// Test 5: Mixed parallel/sequential tools (sequential fallback)
func TestSend_MixedParallelSequential(t *testing.T) {
	calls := []ai.ToolCall{
		{ID: "call_1", Name: "par_a", Arguments: map[string]any{"input": "x"}},
		{ID: "call_2", Name: "echo", Arguments: map[string]any{"input": "y"}},
	}
	registerMock(t,
		toolCallStream(calls, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	// par_a is parallel, echo is not — should fall back to sequential.
	a := New(
		WithModel(testModel()),
		WithTools(parallelEchoTool("par_a"), echoTool()),
	)

	events := collectEvents(t, a, func() {
		require.NoError(t, a.Send(t.Context(), "mixed"))
	})

	// Verify sequential ordering: tool_execution_end for call_1 before
	// tool_execution_start for call_2.
	var toolEndIdx, toolStartIdx int
	for i, e := range events {
		if e.Type == EventToolExecutionEnd && e.ToolCallID == "call_1" {
			toolEndIdx = i
		}
		if e.Type == EventToolExecutionStart && e.ToolCallID == "call_2" {
			toolStartIdx = i
		}
	}
	assert.Greater(t, toolStartIdx, toolEndIdx, "sequential: call_2 should start after call_1 ends")
}

// Test 6: maxTurns reached mid-loop
func TestSend_MaxTurnsReached(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "loop"},
	}
	// Provide enough responses to loop forever, but maxTurns=1 should stop after 1.
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("unreachable", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(echoTool()), WithMaxTurns(1))
	msgs, err := sendAndWait(t, a, "loop")
	require.NoError(t, err)
	// Only 1 turn: assistant (tool call) + tool result. No second turn.
	require.Len(t, msgs, 2)
}

// Test 7: maxTurns=0 (unlimited)
func TestSend_MaxTurnsZero_Unlimited(t *testing.T) {
	call := ai.ToolCall{ID: "call_1", Name: "echo", Arguments: map[string]any{"input": "x"}}
	registerMock(t,
		toolCallStream([]ai.ToolCall{call}, ai.Usage{}),
		toolCallStream([]ai.ToolCall{call}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(echoTool())) // maxTurns=0 by default
	msgs, err := sendAndWait(t, a, "go")
	require.NoError(t, err)
	// 3 turns: tool+result, tool+result, final text
	require.Len(t, msgs, 5)
}

// Test 8: Context cancellation before first turn
func TestSend_ContextCanceledBeforeFirstTurn(t *testing.T) {
	registerMock(t, textStream("should not reach", ai.Usage{}))

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately.

	a := New(WithModel(testModel()))
	err := a.Send(ctx, "hi")
	require.NoError(t, err)

	_, err = a.Wait(t.Context())
	assert.Error(t, err)
	assert.False(t, a.IsRunning())
}

// Test 9: Context cancellation mid-stream
func TestSend_ContextCanceledMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	mock := &mockProvider{
		responses: []*ai.EventStream{blockingStream(ctx)},
	}
	ai.RegisterProvider(mockAPI, mock)
	t.Cleanup(ai.ClearProviders)

	a := New(WithModel(testModel()))
	err := a.Send(ctx, "hi")
	require.NoError(t, err)

	time.AfterFunc(50*time.Millisecond, cancel)

	_, err = a.Wait(t.Context())
	assert.Error(t, err)
	assert.False(t, a.IsRunning())
}

// Test 10: Already streaming guard
func TestSend_AlreadyStreaming(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mock := &mockProvider{
		responses: []*ai.EventStream{blockingStream(ctx)},
	}
	ai.RegisterProvider(mockAPI, mock)
	t.Cleanup(ai.ClearProviders)

	a := New(WithModel(testModel()))
	err := a.Send(ctx, "first")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	err = a.Send(ctx, "second")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "streaming")
}

// Test 11: No provider registered
func TestSend_NoProviderRegistered(t *testing.T) {
	// Don't register any provider.
	ai.ClearProviders()

	a := New(WithModel(testModel()))
	err := a.Send(t.Context(), "hi")
	require.NoError(t, err)

	_, err = a.Wait(t.Context())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no provider")
}

// Test 12: Provider returns error event
func TestSend_ProviderError(t *testing.T) {
	registerMock(t, errorStream(errors.New("rate limit exceeded")))

	a := New(WithModel(testModel()))
	err := a.Send(t.Context(), "hi")
	require.NoError(t, err)

	_, err = a.Wait(t.Context())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

// Test 13: Nil/empty message from provider
func TestSend_NilMessageFromProvider(t *testing.T) {
	// Stream that emits Done with nil message.
	nilMsgStream := ai.NewEventStream(func(push func(ai.Event)) {
		push(ai.Event{Type: ai.EventDone, Message: nil})
	})
	registerMock(t, nilMsgStream)

	a := New(WithModel(testModel()))
	err := a.Send(t.Context(), "hi")
	require.NoError(t, err)

	_, err = a.Wait(t.Context())
	assert.Error(t, err)
}

// Test 14: Unknown tool name → ErrorToolResultMessage
func TestSend_UnknownTool(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "nonexistent",
		Arguments: map[string]any{},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("ok", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(echoTool()))
	msgs, err := sendAndWait(t, a, "call unknown")
	require.NoError(t, err)

	toolResult := findToolResult(t, msgs)
	assert.True(t, toolResult.IsError)
	assert.Equal(t, "call_1", toolResult.ToolCallID)
}

// Test 15: Tool returns error → ErrorToolResultMessage
func TestSend_ToolReturnsError(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "fail",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("handled", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(errorTool()))
	msgs, err := sendAndWait(t, a, "do it")
	require.NoError(t, err) // Agent should NOT error — tool error is non-fatal.

	toolResult := findToolResult(t, msgs)
	assert.True(t, toolResult.IsError)
}

// Test 16: Tool panics → recovered, ErrorToolResultMessage
func TestSend_ToolPanics(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "panic_tool",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("recovered", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(panicTool()))
	msgs, err := sendAndWait(t, a, "panic")
	require.NoError(t, err) // Should recover, not crash.

	toolResult := findToolResult(t, msgs)
	assert.True(t, toolResult.IsError)
}

// Test 17: System prompt is forwarded to the provider
func TestSend_SystemPromptRendering(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	a := New(
		WithModel(testModel()),
		WithSystemPrompt("You are a helper.\n\nBe concise."),
	)
	_, err := sendAndWait(t, a, "hi")
	require.NoError(t, err)

	require.Len(t, mock.prompts, 1)
	assert.Equal(t, "You are a helper.\n\nBe concise.", mock.prompts[0].System)
}

// Test 18: Empty system prompt
func TestSend_EmptySystemPrompt(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	a := New(WithModel(testModel())) // No system prompt.
	_, err := sendAndWait(t, a, "hi")
	require.NoError(t, err)

	require.Len(t, mock.prompts, 1)
	assert.Empty(t, mock.prompts[0].System)
}

// Test 19: Event lifecycle ordering (full sequence verification)
func TestSend_EventLifecycleOrdering(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(echoTool()))

	events := collectEvents(t, a, func() {
		require.NoError(t, a.Send(t.Context(), "go"))
	})
	types := eventTypes(events)

	// Expected lifecycle:
	// session_init
	// agent_start
	//   turn_start
	//     message_start (assistant), message_update*, message_end (assistant)
	//     tool_execution_start, tool_execution_end
	//     message_start (tool result), message_end (tool result)
	//   turn_end
	//   turn_start
	//     message_start (assistant), message_update*, message_end (assistant)
	//   turn_end
	// agent_end

	// Verify key ordering invariants.
	require.GreaterOrEqual(t, len(types), 2)
	assert.Equal(t, EventSessionInit, types[0])
	assert.Equal(t, EventAgentStart, types[1])
	assert.Equal(t, EventAgentEnd, types[len(types)-1])

	// Every turn_start has a matching turn_end.
	turnStarts := 0
	turnEnds := 0
	for _, typ := range types {
		if typ == EventTurnStart {
			turnStarts++
		}
		if typ == EventTurnEnd {
			turnEnds++
		}
	}
	assert.Equal(t, turnStarts, turnEnds, "every TurnStart must have a matching TurnEnd")
	assert.Equal(t, 2, turnStarts, "expected 2 turns")

	// Tool execution events are between turn_start and turn_end.
	assert.Contains(t, types, EventToolExecutionStart)
	assert.Contains(t, types, EventToolExecutionEnd)
}

// Test 20: Message events for user and tool_result messages
func TestSend_MessageEventsForAllTypes(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(echoTool()))

	events := collectEvents(t, a, func() {
		require.NoError(t, a.Send(t.Context(), "go"))
	})

	// Count message_start events — caller input is not echoed; only
	// messages produced by the loop count.
	msgStarts := 0
	msgEnds := 0
	for _, e := range events {
		if e.Type == EventMessageStart {
			msgStarts++
		}
		if e.Type == EventMessageEnd {
			msgEnds++
		}
	}

	// assistant (tool call) + tool result + assistant (final) = 3 message pairs
	assert.Equal(t, 3, msgStarts)
	assert.Equal(t, msgStarts, msgEnds, "every message_start must have a matching message_end")
}

// Test 21: State snapshots during loop
func TestSend_StateSnapshots(t *testing.T) {
	started := make(chan struct{})
	proceed := make(chan struct{})

	// Provider that signals when called and waits.
	mock := &mockProvider{}
	mock.responses = []*ai.EventStream{
		ai.NewEventStream(func(push func(ai.Event)) {
			close(started) // Signal that streaming started.
			<-proceed      // Wait for test to check state.
			msg := &ai.Message{
				Role:       ai.RoleAssistant,
				Content:    []ai.Content{ai.Text{Text: "hi"}},
				StopReason: ai.StopReasonStop,
			}
			push(ai.Event{Type: ai.EventStart, Message: msg})
			push(ai.Event{Type: ai.EventDone, Message: msg})
		}),
	}
	ai.RegisterProvider(mockAPI, mock)
	t.Cleanup(ai.ClearProviders)

	a := New(WithModel(testModel()))
	err := a.Send(t.Context(), "hi")
	require.NoError(t, err)

	<-started
	assert.True(t, a.IsRunning())
	close(proceed)

	_, err = a.Wait(t.Context())
	require.NoError(t, err)
	assert.False(t, a.IsRunning())
}

// Test 22: Wait() returns all new messages
func TestSend_ResultReturnsNewMessages(t *testing.T) {
	registerMock(t, textStream("response", ai.Usage{}))

	history := []Message{
		NewLLMMessage(ai.UserMessage("old")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "old reply"})),
	}
	a := New(WithModel(testModel()), WithHistory(history...))
	msgs, err := sendAndWait(t, a, "new")
	require.NoError(t, err)

	// Result should only contain NEW messages from this run, not history.
	require.Len(t, msgs, 1)
	assert.Equal(t, ai.RoleAssistant, msgs[0].Role)

	// Full history should include old + new user + new assistant.
	assert.Len(t, a.Messages(), 4)
}

// Test 23: Usage accumulation across turns
func TestSend_UsageAccumulation(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "x"},
	}
	registerMock(t,
		toolCallStream(
			[]ai.ToolCall{toolCall},
			ai.Usage{Input: 10, Output: 5, Total: 15},
		),
		textStream(
			"done",
			ai.Usage{Input: 20, Output: 10, Total: 30},
		),
	)

	a := New(WithModel(testModel()), WithTools(echoTool()))

	events := collectEvents(t, a, func() {
		require.NoError(t, a.Send(t.Context(), "go"))
	})

	// Find the agent_end event.
	var agentEnd *Event
	for i := range events {
		if events[i].Type == EventAgentEnd {
			agentEnd = &events[i]
			break
		}
	}
	require.NotNil(t, agentEnd)
	assert.Equal(t, 30, agentEnd.Usage.Input)
	assert.Equal(t, 15, agentEnd.Usage.Output)
	assert.Equal(t, 45, agentEnd.Usage.Total)
}

// Test 24: Continue() with existing history
func TestContinue_WithExistingHistory(t *testing.T) {
	registerMock(t, textStream("continued", ai.Usage{}))

	history := []Message{
		NewLLMMessage(ai.UserMessage("hello")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "hi, what next?"})),
	}
	a := New(WithModel(testModel()), WithHistory(history...))
	err := a.Continue(t.Context())
	require.NoError(t, err)

	msgs, err := a.Wait(t.Context())
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	// History should be original + new assistant.
	assert.Len(t, a.Messages(), 3)
}

// Test 25: Send after previous error (re-entrancy)
func TestSend_AfterPreviousError(t *testing.T) {
	registerMock(t,
		errorStream(errors.New("first error")),
		textStream("recovered", ai.Usage{}),
	)

	a := New(WithModel(testModel()))

	// First call errors.
	err := a.Send(t.Context(), "first")
	require.NoError(t, err)
	_, err = a.Wait(t.Context())
	assert.Error(t, err)
	assert.False(t, a.IsRunning())

	// Second call should work.
	msgs, err := sendAndWait(t, a, "second")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
}

// Test 27: Context cancellation mid-tool-execution (parallel)
func TestSend_ContextCanceledMidToolExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	calls := []ai.ToolCall{
		{ID: "call_1", Name: "blocker", Arguments: map[string]any{"input": "x"}},
	}
	registerMock(t,
		toolCallStream(calls, ai.Usage{}),
		textStream("unreachable", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(blockingTool("blocker")))
	err := a.Send(ctx, "block")
	require.NoError(t, err)

	// Cancel while tool is executing.
	time.AfterFunc(50*time.Millisecond, cancel)

	// The tool should unblock via context cancellation.
	// The loop should complete with an error tool result, then
	// the next turn's ctx.Err() check catches the cancellation.
	msgs, err := a.Wait(t.Context())

	// The agent should terminate (either via error or with tool error result).
	// We accept either outcome — the key invariant is it doesn't hang.
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	} else {
		// If no top-level error, there should be an error tool result.
		var hasErrorResult bool
		for _, m := range msgs {
			if m.IsError {
				hasErrorResult = true
				break
			}
		}
		assert.True(t, hasErrorResult)
	}
	assert.False(t, a.IsRunning())
}

// Verify SendMessages works like Send but with pre-formed messages.
func TestSendMessages(t *testing.T) {
	registerMock(t, textStream("reply", ai.Usage{}))

	a := New(WithModel(testModel()))
	msg := NewLLMMessage(ai.UserMessage("hello"))
	err := a.SendMessages(t.Context(), msg)
	require.NoError(t, err)

	msgs, err := a.Wait(t.Context())
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	// History should have the user message + assistant reply.
	assert.Len(t, a.Messages(), 2)
}

// Verify tool info is passed to the provider.
func TestSend_ToolInfoPassedToProvider(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	a := New(WithModel(testModel()), WithTools(echoTool()))
	_, err := sendAndWait(t, a, "hi")
	require.NoError(t, err)

	require.Len(t, mock.prompts, 1)
	require.Len(t, mock.prompts[0].Tools, 1)
	assert.Equal(t, "echo", mock.prompts[0].Tools[0].Name)
}

// Verify tool result content.
func TestSend_ToolResultContent(t *testing.T) {
	toolCall := ai.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: map[string]any{"input": "hello world"},
	}
	registerMock(t,
		toolCallStream([]ai.ToolCall{toolCall}, ai.Usage{}),
		textStream("done", ai.Usage{}),
	)

	a := New(WithModel(testModel()), WithTools(echoTool()))
	msgs, err := sendAndWait(t, a, "echo")
	require.NoError(t, err)

	toolResult := findToolResult(t, msgs)
	assert.Equal(t, "call_1", toolResult.ToolCallID)
	assert.Equal(t, "echo", toolResult.ToolName)
	assert.False(t, toolResult.IsError)

	// The echo tool returns the input string. marshalToolOutput returns
	// string outputs as plain text via NewTextResult.
	require.Len(t, toolResult.Content, 1)
	text, ok := ai.AsContent[ai.Text](toolResult.Content[0])
	require.True(t, ok)
	assert.Equal(t, "hello world", text.Text)
}

// Verify message_update events carry streaming content.
func TestSend_StreamMessageUpdates(t *testing.T) {
	registerMock(t, textStream("hello", ai.Usage{}))

	a := New(WithModel(testModel()))

	var updateMsgSeen bool
	events := collectEvents(t, a, func() {
		require.NoError(t, a.Send(t.Context(), "hi"))
	})
	for _, event := range events {
		if event.Type == EventMessageUpdate && event.Message != nil {
			updateMsgSeen = true
		}
	}

	assert.True(t, updateMsgSeen, "message_update events should carry a non-nil Message")
	assert.False(t, a.IsRunning())
}

// Test: Multi-turn subscribe — one subscriber sees events from multiple Send calls.
func TestSubscribe_MultiTurn(t *testing.T) {
	registerMock(t,
		textStream("first reply", ai.Usage{}),
		textStream("second reply", ai.Usage{}),
	)

	a := New(WithModel(testModel()))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := a.Subscribe(ctx)

	// First Send.
	require.NoError(t, a.Send(t.Context(), "hello"))
	var firstEvents []Event
	for pe := range ch {
		evt := pe.Payload()
		firstEvents = append(firstEvents, evt)
		if evt.Type == EventAgentEnd {
			break
		}
	}
	// First run leads with session_init (once per agent lifetime), then agent_start.
	require.GreaterOrEqual(t, len(firstEvents), 2)
	assert.Equal(t, EventSessionInit, firstEvents[0].Type)
	assert.Equal(t, EventAgentStart, firstEvents[1].Type)
	assert.Equal(t, EventAgentEnd, firstEvents[len(firstEvents)-1].Type)

	// Second Send on the same subscriber — no second session_init.
	require.NoError(t, a.Send(t.Context(), "world"))
	var secondEvents []Event
	for pe := range ch {
		evt := pe.Payload()
		secondEvents = append(secondEvents, evt)
		if evt.Type == EventAgentEnd {
			break
		}
	}
	assert.Equal(t, EventAgentStart, secondEvents[0].Type)
	assert.Equal(t, EventAgentEnd, secondEvents[len(secondEvents)-1].Type)
	for _, evt := range secondEvents {
		assert.NotEqual(t, EventSessionInit, evt.Type,
			"session_init must fire only once per agent lifetime",
		)
	}

	// History should have both turns.
	assert.Len(t, a.Messages(), 4) // user+assistant + user+assistant
}

// Test: Wait on a fresh agent returns (nil, nil).
func TestWait_FreshAgent(t *testing.T) {
	a := New(WithModel(testModel()))

	msgs, err := a.Wait(t.Context())
	assert.NoError(t, err)
	assert.Nil(t, msgs)
}

// Test: Incremental streaming — message_start fires on first delta,
// every message_update carries a non-nil partial Message snapshot,
// and message_end carries the final provider message.
func TestSend_IncrementalStreamingEvents(t *testing.T) {
	// Create a realistic multi-delta stream (Message only on EventDone).
	stream := ai.NewEventStream(func(push func(ai.Event)) {
		finalMsg := &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    []ai.Content{ai.Text{Text: "Hello world"}},
			StopReason: ai.StopReasonStop,
			Usage:      ai.Usage{Input: 10, Output: 5, Total: 15},
		}
		push(ai.Event{Type: ai.EventTextStart, ContentIndex: 0})
		push(ai.Event{Type: ai.EventTextDelta, ContentIndex: 0, Delta: "Hello"})
		push(ai.Event{Type: ai.EventTextDelta, ContentIndex: 0, Delta: " world"})
		push(ai.Event{Type: ai.EventTextEnd, ContentIndex: 0, Content: "Hello world"})
		push(ai.Event{Type: ai.EventDone, Message: finalMsg, StopReason: ai.StopReasonStop})
	})
	registerMock(t, stream)

	a := New(WithModel(testModel()))

	events := collectEvents(t, a, func() {
		require.NoError(t, a.Send(t.Context(), "hi"))
	})

	// Filter to assistant message events only.
	var msgEvents []Event
	for _, e := range events {
		switch e.Type {
		case EventMessageStart, EventMessageUpdate, EventMessageEnd:
			if e.Message != nil && e.Message.Role == ai.RoleAssistant {
				msgEvents = append(msgEvents, e)
			}
		}
	}

	require.NotEmpty(t, msgEvents, "should have assistant message events")

	// First assistant event should be message_start with an empty-ish partial.
	assert.Equal(t, EventMessageStart, msgEvents[0].Type)
	require.NotNil(t, msgEvents[0].Message)
	assert.Equal(t, ai.RoleAssistant, msgEvents[0].Message.Role)

	// Every message_update should have a non-nil Message (partial snapshot).
	for _, e := range msgEvents {
		if e.Type == EventMessageUpdate {
			require.NotNil(t, e.Message, "message_update must carry a non-nil Message snapshot")
			assert.Equal(t, ai.RoleAssistant, e.Message.Role)
		}
	}

	// Last assistant event should be message_end with the final text.
	last := msgEvents[len(msgEvents)-1]
	assert.Equal(t, EventMessageEnd, last.Type)
	require.NotNil(t, last.Message)
	assert.Equal(t, "Hello world", last.Message.Text())

	// Verify partial accumulation: the last message_update before message_end
	// should have accumulated "Hello world".
	var lastUpdate *Event
	for i := range msgEvents {
		if msgEvents[i].Type == EventMessageUpdate {
			lastUpdate = &msgEvents[i]
		}
	}
	require.NotNil(t, lastUpdate)
	assert.Equal(t, "Hello world", lastUpdate.Message.Text())
}

// Test: Multi-block streaming — thinking + text blocks accumulate correctly.
func TestSend_MultiBlockStreaming(t *testing.T) {
	stream := ai.NewEventStream(func(push func(ai.Event)) {
		finalMsg := &ai.Message{
			Role: ai.RoleAssistant,
			Content: []ai.Content{
				ai.Thinking{Thinking: "Let me think..."},
				ai.Text{Text: "The answer is 42"},
			},
			StopReason: ai.StopReasonStop,
		}
		// Block 0: thinking
		push(ai.Event{Type: ai.EventThinkStart, ContentIndex: 0})
		push(ai.Event{Type: ai.EventThinkDelta, ContentIndex: 0, Delta: "Let me "})
		push(ai.Event{Type: ai.EventThinkDelta, ContentIndex: 0, Delta: "think..."})
		push(ai.Event{Type: ai.EventThinkEnd, ContentIndex: 0, Content: "Let me think..."})
		// Block 1: text
		push(ai.Event{Type: ai.EventTextStart, ContentIndex: 1})
		push(ai.Event{Type: ai.EventTextDelta, ContentIndex: 1, Delta: "The answer"})
		push(ai.Event{Type: ai.EventTextDelta, ContentIndex: 1, Delta: " is 42"})
		push(ai.Event{Type: ai.EventTextEnd, ContentIndex: 1, Content: "The answer is 42"})
		push(ai.Event{Type: ai.EventDone, Message: finalMsg, StopReason: ai.StopReasonStop})
	})
	registerMock(t, stream)

	a := New(WithModel(testModel()))

	events := collectEvents(t, a, func() {
		require.NoError(t, a.Send(t.Context(), "think"))
	})

	// Find the last message_update before message_end for the assistant.
	var lastUpdate *Event
	for i := range events {
		if events[i].Type == EventMessageUpdate &&
			events[i].Message != nil &&
			events[i].Message.Role == ai.RoleAssistant {
			lastUpdate = &events[i]
		}
	}
	require.NotNil(t, lastUpdate)

	// Partial should have both blocks accumulated.
	require.Len(t, lastUpdate.Message.Content, 2)

	think, ok := ai.AsContent[ai.Thinking](lastUpdate.Message.Content[0])
	require.True(t, ok)
	assert.Equal(t, "Let me think...", think.Thinking)

	text, ok := ai.AsContent[ai.Text](lastUpdate.Message.Content[1])
	require.True(t, ok)
	assert.Equal(t, "The answer is 42", text.Text)
}

// Test: Message snapshots are independent copies (mutation safety).
func TestSend_SnapshotIndependence(t *testing.T) {
	stream := ai.NewEventStream(func(push func(ai.Event)) {
		finalMsg := &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    []ai.Content{ai.Text{Text: "ab"}},
			StopReason: ai.StopReasonStop,
		}
		push(ai.Event{Type: ai.EventTextStart, ContentIndex: 0})
		push(ai.Event{Type: ai.EventTextDelta, ContentIndex: 0, Delta: "a"})
		push(ai.Event{Type: ai.EventTextDelta, ContentIndex: 0, Delta: "b"})
		push(ai.Event{Type: ai.EventTextEnd, ContentIndex: 0, Content: "ab"})
		push(ai.Event{Type: ai.EventDone, Message: finalMsg, StopReason: ai.StopReasonStop})
	})
	registerMock(t, stream)

	a := New(WithModel(testModel()))

	events := collectEvents(t, a, func() {
		require.NoError(t, a.Send(t.Context(), "hi"))
	})

	// Collect all message_update snapshots for the assistant.
	var snapshots []*ai.Message
	for _, e := range events {
		if e.Type == EventMessageUpdate &&
			e.Message != nil &&
			e.Message.Role == ai.RoleAssistant {
			snapshots = append(snapshots, e.Message)
		}
	}

	require.GreaterOrEqual(t, len(snapshots), 2, "need at least 2 snapshots")

	// Earlier snapshots should not be mutated by later accumulation.
	// The first delta "a" snapshot should still show "a", not "ab".
	firstText := snapshots[0].Text()
	lastText := snapshots[len(snapshots)-1].Text()
	assert.NotEqual(t, firstText, lastText, "snapshots should differ (not aliased)")
}

// TestSessionLifecycle_SendThenClose verifies that session_init fires
// exactly once on the first run and session_end fires exactly once on
// Close, wrapping all per-run agent_start/agent_end pairs.
func TestSessionLifecycle_SendThenClose(t *testing.T) {
	registerMock(t,
		textStream("first reply", ai.Usage{}),
		textStream("second reply", ai.Usage{}),
	)

	a := New(WithModel(testModel()))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := a.Subscribe(ctx)

	require.NoError(t, a.Send(t.Context(), "one"))
	_, err := a.Wait(t.Context())
	require.NoError(t, err)

	require.NoError(t, a.Send(t.Context(), "two"))
	_, err = a.Wait(t.Context())
	require.NoError(t, err)

	a.Close()

	var events []Event
	for pe := range ch {
		events = append(events, pe.Payload())
	}

	var inits, ends, agentStarts, agentEnds int
	for _, e := range events {
		switch e.Type {
		case EventSessionInit:
			inits++
		case EventSessionEnd:
			ends++
		case EventAgentStart:
			agentStarts++
		case EventAgentEnd:
			agentEnds++
		}
	}
	assert.Equal(t, 1, inits, "session_init fires once")
	assert.Equal(t, 1, ends, "session_end fires once")
	assert.Equal(t, 2, agentStarts, "agent_start fires once per Send")
	assert.Equal(t, 2, agentEnds, "agent_end fires once per Send")

	require.GreaterOrEqual(t, len(events), 1)
	assert.Equal(t, EventSessionInit, events[0].Type)
	assert.Equal(t, EventSessionEnd, events[len(events)-1].Type)
}

// TestSessionLifecycle_CloseWithoutSend verifies that closing a never-used
// agent emits no session events at all.
func TestSessionLifecycle_CloseWithoutSend(t *testing.T) {
	a := New(WithModel(testModel()))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := a.Subscribe(ctx)

	a.Close()

	var events []Event
	for pe := range ch {
		events = append(events, pe.Payload())
	}

	assert.Empty(t, events, "Close on a never-used agent should publish no events")
}

// TestSessionLifecycle_CloseWithoutWait verifies that calling Close
// immediately after Send (without Wait) is race-free and still preserves
// the documented session_end-after-agent_end ordering. Run with -race
// to catch regressions of the sessionInitFired data race.
func TestSessionLifecycle_CloseWithoutWait(t *testing.T) {
	registerMock(t, textStream("hi", ai.Usage{}))

	a := New(WithModel(testModel()))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := a.Subscribe(ctx)

	require.NoError(t, a.Send(t.Context(), "x"))
	a.Close()

	var events []Event
	for pe := range ch {
		events = append(events, pe.Payload())
	}

	require.NotEmpty(t, events)

	var lastAgentEnd, sessionEnd int = -1, -1
	for i, e := range events {
		switch e.Type {
		case EventAgentEnd:
			lastAgentEnd = i
		case EventSessionEnd:
			sessionEnd = i
		}
	}
	require.NotEqual(t, -1, lastAgentEnd, "agent_end must fire")
	require.NotEqual(t, -1, sessionEnd, "session_end must fire")
	assert.Less(t, lastAgentEnd, sessionEnd,
		"session_end must come after the final agent_end",
	)
	assert.Equal(t, EventSessionEnd, events[len(events)-1].Type)
}
