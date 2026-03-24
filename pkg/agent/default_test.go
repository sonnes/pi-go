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

// textStream creates an [ai.EventStream] that streams a text response.
func textStream(text string, usage ai.Usage) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) {
		msg := &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    []ai.Content{ai.Text{Text: text}},
			StopReason: ai.StopReasonStop,
			Usage:      usage,
		}
		push(ai.Event{Type: ai.EventStart, Message: msg})
		push(ai.Event{Type: ai.EventTextDelta, Delta: text, Message: msg})
		push(ai.Event{Type: ai.EventDone, Message: msg, StopReason: ai.StopReasonStop})
	})
}

// toolCallStream creates an [ai.EventStream] that returns tool call(s).
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
		push(ai.Event{Type: ai.EventStart, Message: msg})
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

// collectEvents drains all events from a stream and returns them.
func collectEvents(t *testing.T, stream *EventStream) []Event {
	t.Helper()
	var events []Event
	for event, err := range stream.Events(t.Context()) {
		require.NoError(t, err)
		events = append(events, event)
	}
	return events
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

// staticSection implements [PromptSection] with a fixed key and content.
type staticSection struct {
	key     string
	content string
}

func (s staticSection) Key() string                      { return s.key }
func (s staticSection) Content(_ context.Context) string { return s.content }

// panicSection implements [PromptSection] that panics.
type panicSection struct{}

func (panicSection) Key() string                      { return "panic" }
func (panicSection) Content(_ context.Context) string { panic("section panic") }

// --- Existing constructor tests ---

func TestNewDefault_Defaults(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	a := New(model)

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

	a := New(model, WithTools(tool))

	assert.Len(t, a.config.tools, 1)
}

func TestNewDefault_WithHistory(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	msgs := []Message{
		NewLLMMessage(ai.UserMessage("hello")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "hi"})),
	}

	a := New(model, WithHistory(msgs...))

	assert.Equal(t, msgs, a.Messages())
}

func TestNewDefault_WithHistory_IsCopied(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	msgs := []Message{NewLLMMessage(ai.UserMessage("hello"))}

	a := New(model, WithHistory(msgs...))

	// Mutate original — should not affect agent state.
	msgs[0] = NewLLMMessage(ai.UserMessage("modified"))

	got := a.Messages()
	lm, ok := AsLLMMessage(got[0])
	assert.True(t, ok)
	assert.Equal(t, "hello", lm.Content[0].(ai.Text).Text)
}

func TestNewDefault_WithMaxTurns(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	a := New(model, WithMaxTurns(5))

	assert.Equal(t, 5, a.config.maxTurns)
}

func TestNewDefault_WithSystemPrompt(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	prompt := Prompt{Sections: []PromptSection{}}

	a := New(model, WithSystemPrompt(prompt))

	assert.Equal(t, prompt, a.config.systemPrompt)
}

func TestNewDefault_WithStreamOpts(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	opts := []ai.Option{ai.WithMaxTokens(100)}

	a := New(model, WithStreamOpts(opts...))

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
		model,
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

	a := New(testModel())
	stream := a.Send(t.Context(), "hi")

	msgs, err := stream.Result()
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

	a := New(testModel(), WithTools(echoTool()))
	stream := a.Send(t.Context(), "call echo")

	msgs, err := stream.Result()
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

	a := New(testModel(), WithTools(echoTool()))
	stream := a.Send(t.Context(), "do two things")

	msgs, err := stream.Result()
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
		testModel(),
		WithTools(parallelEchoTool("par_a"), parallelEchoTool("par_b")),
	)
	stream := a.Send(t.Context(), "parallel")

	msgs, err := stream.Result()
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
		testModel(),
		WithTools(parallelEchoTool("par_a"), echoTool()),
	)
	stream := a.Send(t.Context(), "mixed")

	events := collectEvents(t, stream)

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

	a := New(testModel(), WithTools(echoTool()), WithMaxTurns(1))
	stream := a.Send(t.Context(), "loop")

	msgs, err := stream.Result()
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

	a := New(testModel(), WithTools(echoTool())) // maxTurns=0 by default
	stream := a.Send(t.Context(), "go")

	msgs, err := stream.Result()
	require.NoError(t, err)
	// 3 turns: tool+result, tool+result, final text
	require.Len(t, msgs, 5)
}

// Test 8: Context cancellation before first turn
func TestSend_ContextCanceledBeforeFirstTurn(t *testing.T) {
	registerMock(t, textStream("should not reach", ai.Usage{}))

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately.

	a := New(testModel())
	stream := a.Send(ctx, "hi")

	_, err := stream.Result()
	assert.Error(t, err)
	assert.False(t, a.IsRunning())
}

// Test 9: Context cancellation mid-stream
func TestSend_ContextCanceledMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	// Provider blocks until context is canceled.
	mock := &mockProvider{
		responses: []*ai.EventStream{blockingStream(ctx)},
	}
	ai.RegisterProvider(mockAPI, mock)
	t.Cleanup(ai.ClearProviders)

	a := New(testModel())
	stream := a.Send(ctx, "hi")

	// Cancel after a short delay to ensure the loop started.
	time.AfterFunc(50*time.Millisecond, cancel)

	_, err := stream.Result()
	assert.Error(t, err)
	assert.False(t, a.IsRunning())
}

// Test 10: Already streaming guard
func TestSend_AlreadyStreaming(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// First call blocks forever.
	mock := &mockProvider{
		responses: []*ai.EventStream{blockingStream(ctx)},
	}
	ai.RegisterProvider(mockAPI, mock)
	t.Cleanup(ai.ClearProviders)

	a := New(testModel())
	_ = a.Send(ctx, "first")

	// Give the goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	// Second call should fail immediately.
	stream2 := a.Send(ctx, "second")
	_, err := stream2.Result()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "streaming")
}

// Test 11: No provider registered
func TestSend_NoProviderRegistered(t *testing.T) {
	// Don't register any provider.
	ai.ClearProviders()

	a := New(testModel())
	stream := a.Send(t.Context(), "hi")

	_, err := stream.Result()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no provider")
}

// Test 12: Provider returns error event
func TestSend_ProviderError(t *testing.T) {
	registerMock(t, errorStream(errors.New("rate limit exceeded")))

	a := New(testModel())
	stream := a.Send(t.Context(), "hi")

	_, err := stream.Result()
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

	a := New(testModel())
	stream := a.Send(t.Context(), "hi")

	_, err := stream.Result()
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

	a := New(testModel(), WithTools(echoTool()))
	stream := a.Send(t.Context(), "call unknown")

	msgs, err := stream.Result()
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

	a := New(testModel(), WithTools(errorTool()))
	stream := a.Send(t.Context(), "do it")

	msgs, err := stream.Result()
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

	a := New(testModel(), WithTools(panicTool()))
	stream := a.Send(t.Context(), "panic")

	msgs, err := stream.Result()
	require.NoError(t, err) // Should recover, not crash.

	toolResult := findToolResult(t, msgs)
	assert.True(t, toolResult.IsError)
}

// Test 17: System prompt rendering (multiple sections)
func TestSend_SystemPromptRendering(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	prompt := Prompt{
		Sections: []PromptSection{
			staticSection{key: "role", content: "You are a helper."},
			staticSection{key: "rules", content: "Be concise."},
		},
	}

	a := New(testModel(), WithSystemPrompt(prompt))
	stream := a.Send(t.Context(), "hi")

	_, err := stream.Result()
	require.NoError(t, err)

	require.Len(t, mock.prompts, 1)
	assert.Contains(t, mock.prompts[0].System, "You are a helper.")
	assert.Contains(t, mock.prompts[0].System, "Be concise.")
}

// Test 18: Empty system prompt (no sections)
func TestSend_EmptySystemPrompt(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	a := New(testModel()) // No system prompt.
	stream := a.Send(t.Context(), "hi")

	_, err := stream.Result()
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

	a := New(testModel(), WithTools(echoTool()))
	stream := a.Send(t.Context(), "go")

	events := collectEvents(t, stream)
	types := eventTypes(events)

	// Expected lifecycle:
	// agent_start
	//   message_start (user), message_end (user)
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
	assert.Equal(t, EventAgentStart, types[0])
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

	a := New(testModel(), WithTools(echoTool()))
	stream := a.Send(t.Context(), "go")

	events := collectEvents(t, stream)

	// Count message_start events — should include user, assistant(s), tool_result.
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

	// user + assistant (tool call) + tool result + assistant (final) = 4 message pairs
	assert.Equal(t, 4, msgStarts)
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

	a := New(testModel())
	stream := a.Send(t.Context(), "hi")

	// Wait for the provider to be called.
	<-started

	assert.True(t, a.IsRunning())

	close(proceed)

	_, err := stream.Result()
	require.NoError(t, err)

	assert.False(t, a.IsRunning())
}

// Test 22: Result() returns all new messages
func TestSend_ResultReturnsNewMessages(t *testing.T) {
	registerMock(t, textStream("response", ai.Usage{}))

	history := []Message{
		NewLLMMessage(ai.UserMessage("old")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "old reply"})),
	}
	a := New(testModel(), WithHistory(history...))
	stream := a.Send(t.Context(), "new")

	msgs, err := stream.Result()
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

	a := New(testModel(), WithTools(echoTool()))
	stream := a.Send(t.Context(), "go")

	events := collectEvents(t, stream)

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
	a := New(testModel(), WithHistory(history...))
	stream := a.Continue(t.Context())

	msgs, err := stream.Result()
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

	a := New(testModel())

	// First call errors.
	_, err := a.Send(t.Context(), "first").Result()
	assert.Error(t, err)
	assert.False(t, a.IsRunning())

	// Second call should work.
	msgs, err := a.Send(t.Context(), "second").Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
}

// Test 26: Section.Content() panics → recovered, error state
func TestSend_SectionPanics(t *testing.T) {
	registerMock(t, textStream("unreachable", ai.Usage{}))

	a := New(
		testModel(),
		WithSystemPrompt(Prompt{Sections: []PromptSection{panicSection{}}}),
	)
	stream := a.Send(t.Context(), "hi")

	_, err := stream.Result()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic")
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

	a := New(testModel(), WithTools(blockingTool("blocker")))
	stream := a.Send(ctx, "block")

	// Cancel while tool is executing.
	time.AfterFunc(50*time.Millisecond, cancel)

	// The tool should unblock via context cancellation.
	// The loop should complete with an error tool result, then
	// the next turn's ctx.Err() check catches the cancellation.
	msgs, err := stream.Result()

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

	a := New(testModel())
	msg := NewLLMMessage(ai.UserMessage("hello"))
	stream := a.SendMessages(t.Context(), msg)

	msgs, err := stream.Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	// History should have the user message + assistant reply.
	assert.Len(t, a.Messages(), 2)
}

// Verify tool info is passed to the provider.
func TestSend_ToolInfoPassedToProvider(t *testing.T) {
	mock := registerMock(t, textStream("ok", ai.Usage{}))

	a := New(testModel(), WithTools(echoTool()))
	_, err := a.Send(t.Context(), "hi").Result()
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

	a := New(testModel(), WithTools(echoTool()))
	msgs, err := a.Send(t.Context(), "echo").Result()
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

	a := New(testModel())
	stream := a.Send(t.Context(), "hi")

	var updateMsgSeen bool
	for event, err := range stream.Events(t.Context()) {
		require.NoError(t, err)
		if event.Type == EventMessageUpdate && event.Message != nil {
			updateMsgSeen = true
		}
	}

	assert.True(t, updateMsgSeen, "message_update events should carry a non-nil Message")
	assert.False(t, a.IsRunning())
}
