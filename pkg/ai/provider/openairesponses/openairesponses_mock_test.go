package openairesponses_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	aior "github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
)

// sseServer creates a test server that returns SSE events for the
// Responses API /v1/responses endpoint.
func sseServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
}

func newMockProvider(t *testing.T, events []string) (*aior.Provider, func()) {
	t.Helper()
	srv := sseServer(t, events)
	p := aior.New(
		option.WithAPIKey("fake-key"),
		option.WithBaseURL(srv.URL+"/v1"),
	)
	return p, srv.Close
}

func TestMock_TextGeneration(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","content":[]}}`,
		`{"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`,
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Olá"}`,
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"! Como"}`,
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":" vai?"}`,
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":"Olá! Como vai?"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":20,"output_tokens":5,"total_tokens":25,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	msg, err := p.StreamText(context.Background(), testModel(), ai.Prompt{
		System:   "You are helpful",
		Messages: []ai.Message{ai.UserMessage("Say hi in Portuguese")},
	}, ai.StreamOptions{}).Result()

	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "Olá! Como vai?", msg.Text())
	assert.Equal(t, ai.StopReasonStop, msg.StopReason)
	assert.Equal(t, "openai-responses", msg.API)
	assert.Equal(t, 20, msg.Usage.Input)
	assert.Equal(t, 5, msg.Usage.Output)
	assert.Equal(t, 25, msg.Usage.Total)
}

func TestMock_StreamEventSequence(t *testing.T) {
	events := []string{
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Hi"}`,
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"!"}`,
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":"Hi!"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	stream := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("hi")},
	}, ai.StreamOptions{})

	var types []ai.EventType
	for e, err := range stream.Events() {
		require.NoError(t, err)
		types = append(types, e.Type)
	}

	// TextStart, TextDelta, TextDelta, TextEnd, Done
	require.GreaterOrEqual(t, len(types), 4)
	assert.Equal(t, ai.EventTextStart, types[0])
	assert.Equal(t, ai.EventTextDelta, types[1])
	assert.Equal(t, ai.EventDone, types[len(types)-1])

	// TextEnd before Done
	textEndIdx := -1
	doneIdx := -1
	for i, tt := range types {
		if tt == ai.EventTextEnd {
			textEndIdx = i
		}
		if tt == ai.EventDone {
			doneIdx = i
		}
	}
	assert.Less(t, textEndIdx, doneIdx)
}

func TestMock_ToolCall(t *testing.T) {
	events := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_abc","name":"get_weather","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"loc"}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"ation\":\"NYC\"}"}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"location\":\"NYC\"}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":30,"output_tokens":10,"total_tokens":40,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	msg, err := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("Weather in NYC?")},
		Tools:    []ai.ToolInfo{*weatherToolSchema},
	}, ai.StreamOptions{}).Result()

	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, ai.StopReasonToolUse, msg.StopReason)

	toolCalls := msg.ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "get_weather", toolCalls[0].Name)
	assert.Equal(t, "call_abc", toolCalls[0].ID)
	assert.Equal(t, "NYC", toolCalls[0].Arguments["location"])
}

func TestMock_ToolCallEventSequence(t *testing.T) {
	events := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"location\":\"NYC\"}"}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"location\":\"NYC\"}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	stream := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("weather?")},
		Tools:    []ai.ToolInfo{*weatherToolSchema},
	}, ai.StreamOptions{})

	var types []ai.EventType
	for e, err := range stream.Events() {
		require.NoError(t, err)
		types = append(types, e.Type)
	}

	assert.Contains(t, types, ai.EventToolStart)
	assert.Contains(t, types, ai.EventToolDelta)
	assert.Contains(t, types, ai.EventToolEnd)
	assert.Contains(t, types, ai.EventDone)

	// ToolEnd before Done
	toolEndIdx := -1
	doneIdx := -1
	for i, tt := range types {
		if tt == ai.EventToolEnd {
			toolEndIdx = i
		}
		if tt == ai.EventDone {
			doneIdx = i
		}
	}
	assert.Less(t, toolEndIdx, doneIdx)
}

func TestMock_TextThenToolCall(t *testing.T) {
	// Ported from pi-mono: text output followed by tool call in same response.
	events := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Let me check"}`,
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":"Let me check"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"location\":\"NYC\"}"}`,
		`{"type":"response.function_call_arguments.done","output_index":1,"arguments":"{\"location\":\"NYC\"}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":10,"output_tokens":15,"total_tokens":25,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	stream := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("weather?")},
		Tools:    []ai.ToolInfo{*weatherToolSchema},
	}, ai.StreamOptions{})

	var types []ai.EventType
	for e, err := range stream.Events() {
		require.NoError(t, err)
		types = append(types, e.Type)
	}

	// Should have both text and tool events
	assert.Contains(t, types, ai.EventTextStart)
	assert.Contains(t, types, ai.EventTextEnd)
	assert.Contains(t, types, ai.EventToolStart)
	assert.Contains(t, types, ai.EventToolEnd)

	// TextEnd before ToolStart
	textEndIdx := -1
	toolStartIdx := -1
	for i, tt := range types {
		if tt == ai.EventTextEnd && textEndIdx == -1 {
			textEndIdx = i
		}
		if tt == ai.EventToolStart && toolStartIdx == -1 {
			toolStartIdx = i
		}
	}
	assert.Less(t, textEndIdx, toolStartIdx)
}

func TestMock_Reasoning(t *testing.T) {
	// Ported from pi-mono: reasoning summary events for o-series models.
	events := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`,
		`{"type":"response.reasoning_summary_part.added","output_index":0,"summary_index":0,"part":{"type":"text","text":""}}`,
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":"Let me think"}`,
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":" about this..."}`,
		`{"type":"response.reasoning_summary_part.done","output_index":0,"summary_index":0,"part":{"type":"text","text":"Let me think about this..."}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg_1","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"The answer is 42."}`,
		`{"type":"response.output_text.done","output_index":1,"content_index":0,"text":"The answer is 42."}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":15,"output_tokens":20,"total_tokens":35,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":10}}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	msg, err := p.StreamText(context.Background(), ai.Model{
		ID:       "o3",
		Name:     "o3",
		API:      "openai-responses",
		Provider: "openai",
	}, ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("What is the meaning of life?")},
	}, ai.StreamOptions{
		ThinkingLevel: ai.ThinkingHigh,
	}).Result()

	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, ai.StopReasonStop, msg.StopReason)

	// Should have thinking content
	var hasThinking, hasText bool
	for _, c := range msg.Content {
		if _, ok := ai.AsContent[ai.Thinking](c); ok {
			hasThinking = true
		}
		if _, ok := ai.AsContent[ai.Text](c); ok {
			hasText = true
		}
	}
	assert.True(t, hasThinking, "expected thinking content")
	assert.True(t, hasText, "expected text content")
	assert.Equal(t, "The answer is 42.", msg.Text())
}

func TestMock_ReasoningEventSequence(t *testing.T) {
	events := []string{
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":"Thinking..."}`,
		`{"type":"response.reasoning_summary_part.done","output_index":0,"summary_index":0,"part":{"type":"text","text":"Thinking..."}}`,
		`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"Answer"}`,
		`{"type":"response.output_text.done","output_index":1,"content_index":0,"text":"Answer"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":5}}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	stream := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("think")},
	}, ai.StreamOptions{})

	var types []ai.EventType
	for e, err := range stream.Events() {
		require.NoError(t, err)
		types = append(types, e.Type)
	}

	// ThinkStart, ThinkDelta, ThinkEnd, TextStart, TextDelta, TextEnd, Done
	thinkStartIdx := -1
	thinkEndIdx := -1
	textStartIdx := -1
	for i, tt := range types {
		switch tt {
		case ai.EventThinkStart:
			thinkStartIdx = i
		case ai.EventThinkEnd:
			thinkEndIdx = i
		case ai.EventTextStart:
			textStartIdx = i
		}
	}

	assert.GreaterOrEqual(t, thinkStartIdx, 0, "should have ThinkStart")
	assert.Greater(t, thinkEndIdx, thinkStartIdx, "ThinkEnd after ThinkStart")
	assert.Greater(t, textStartIdx, thinkEndIdx, "TextStart after ThinkEnd")
}

func TestMock_IncompleteResponse(t *testing.T) {
	// Ported from pi-mono: incomplete response maps to StopReasonLength.
	events := []string{
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial..."}`,
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":"partial..."}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"incomplete","usage":{"input_tokens":100,"output_tokens":4096,"total_tokens":4196,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}},"incomplete_details":{"reason":"max_output_tokens"}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	msg, err := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("write a long essay")},
	}, ai.StreamOptions{}).Result()

	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, ai.StopReasonLength, msg.StopReason)
	assert.Equal(t, "partial...", msg.Text())
}

func TestMock_CachedTokens(t *testing.T) {
	events := []string{
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"cached"}`,
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":"cached"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":100,"output_tokens":5,"total_tokens":105,"input_tokens_details":{"cached_tokens":80},"output_tokens_details":{"reasoning_tokens":0}}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	msg, err := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("hi")},
	}, ai.StreamOptions{}).Result()

	require.NoError(t, err)
	assert.Equal(t, 100, msg.Usage.Input)
	assert.Equal(t, 5, msg.Usage.Output)
	assert.Equal(t, 80, msg.Usage.CacheRead)
	assert.Equal(t, 105, msg.Usage.Total)
}

func TestMock_FailedResponse(t *testing.T) {
	events := []string{
		`{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"server_error","message":"Internal error"}}}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	_, err := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("fail")},
	}, ai.StreamOptions{}).Result()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "response failed")
}

func TestMock_ErrorEvent(t *testing.T) {
	events := []string{
		`{"type":"error","code":"invalid_request","message":"Bad request: missing model"}`,
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	_, err := p.StreamText(context.Background(), testModel(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("err")},
	}, ai.StreamOptions{}).Result()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Bad request")
}

func TestMock_MultiTurnToolCallConversion(t *testing.T) {
	// Ported from pi-mono: verify that multi-turn with tool results
	// correctly builds the input items (assistant tool call + tool result).
	events := []string{
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"It's 72F in NYC."}`,
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":"It's 72F in NYC."}`,
		`{"type":"response.completed","response":{"id":"resp_2","status":"completed","usage":{"input_tokens":50,"output_tokens":10,"total_tokens":60,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}`,
	}

	// Capture the request body to verify conversion.
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		capturedBody = string(data)

		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer srv.Close()

	p := aior.New(
		option.WithAPIKey("fake-key"),
		option.WithBaseURL(srv.URL+"/v1"),
	)

	msg, err := p.StreamText(context.Background(), testModel(), ai.Prompt{
		System: "helpful",
		Messages: []ai.Message{
			ai.UserMessage("Weather in NYC?"),
			ai.AssistantMessage(ai.ToolCall{
				ID:        "call_abc",
				Name:      "get_weather",
				Arguments: map[string]any{"location": "NYC"},
			}),
			ai.ToolResultMessage("call_abc", "get_weather",
				ai.Text{Text: `{"temp": 72}`},
			),
		},
		Tools: []ai.ToolInfo{*weatherToolSchema},
	}, ai.StreamOptions{}).Result()

	require.NoError(t, err)
	assert.Equal(t, "It's 72F in NYC.", msg.Text())

	// Verify the request contains function_call and function_call_output items
	assert.Contains(t, capturedBody, "function_call")
	assert.Contains(t, capturedBody, "function_call_output")
	assert.Contains(t, capturedBody, "call_abc")
}
