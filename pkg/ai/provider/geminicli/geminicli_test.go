package geminicli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// Tests use the wrapped SSE format {"response": {...}} matching the
// Cloud Code Assist API, plus the unwrapped format for compatibility.

func TestProcessSSE_TextResponse(t *testing.T) {
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello "}]}}]}}
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"world!"}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	model := ai.Model{ID: "gemini-2.5-flash"}
	p.processSSE(strings.NewReader(sse), push, model)

	var eventTypes []ai.EventType
	for _, e := range events {
		eventTypes = append(eventTypes, e.Type)
	}

	assert.Contains(t, eventTypes, ai.EventTextStart)
	assert.Contains(t, eventTypes, ai.EventTextDelta)
	assert.Contains(t, eventTypes, ai.EventTextEnd)
	assert.Contains(t, eventTypes, ai.EventDone)

	doneEvent := events[len(events)-1]
	require.NotNil(t, doneEvent.Message)
	assert.Equal(t, ai.StopReasonStop, doneEvent.StopReason)
	assert.Equal(t, "Hello world!", doneEvent.Message.Text())
	assert.Equal(t, 10, doneEvent.Message.Usage.Input)
	assert.Equal(t, 5, doneEvent.Message.Usage.Output)
}

func TestProcessSSE_UnwrappedFormat(t *testing.T) {
	// Also support unwrapped format for flexibility.
	sse := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]}}]}
data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	doneEvent := events[len(events)-1]
	require.NotNil(t, doneEvent.Message)
	assert.Equal(t, "hi", doneEvent.Message.Text())
}

func TestProcessSSE_ThinkingThenText(t *testing.T) {
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"thinking...","thought":true}]}}]}}
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"answer","thoughtSignature":"sig123"}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	var eventTypes []ai.EventType
	for _, e := range events {
		eventTypes = append(eventTypes, e.Type)
	}

	thinkStartIdx := -1
	thinkEndIdx := -1
	textStartIdx := -1

	for i, et := range eventTypes {
		switch et {
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

	// Verify thinking content has signature
	doneEvent := events[len(events)-1]
	require.NotNil(t, doneEvent.Message)
	require.GreaterOrEqual(t, len(doneEvent.Message.Content), 2)

	thinking, ok := ai.AsContent[ai.Thinking](doneEvent.Message.Content[0])
	require.True(t, ok)
	assert.Equal(t, "thinking...", thinking.Thinking)
	assert.Equal(t, "sig123", thinking.Signature)
}

func TestProcessSSE_ToolCall(t *testing.T) {
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"location":"NYC"},"id":"call_1"}}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	var eventTypes []ai.EventType
	for _, e := range events {
		eventTypes = append(eventTypes, e.Type)
	}

	assert.Contains(t, eventTypes, ai.EventToolStart)
	assert.Contains(t, eventTypes, ai.EventToolDelta)
	assert.Contains(t, eventTypes, ai.EventToolEnd)

	doneEvent := events[len(events)-1]
	require.NotNil(t, doneEvent.Message)
	assert.Equal(t, ai.StopReasonToolUse, doneEvent.StopReason)

	toolCalls := doneEvent.Message.ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "get_weather", toolCalls[0].Name)
	assert.Equal(t, "call_1", toolCalls[0].ID)
}

func TestProcessSSE_ToolCallMissingArgs(t *testing.T) {
	// Ported from pi-mono: google-tool-call-missing-args.test.ts
	// Tool calls with no args field should default to empty object.
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_status","id":"call_1"}}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	doneEvent := events[len(events)-1]
	require.NotNil(t, doneEvent.Message)
	assert.Equal(t, ai.StopReasonToolUse, doneEvent.StopReason)

	toolCalls := doneEvent.Message.ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "get_status", toolCalls[0].Name)
	// Args should be nil (no args field), not cause a crash
	assert.Nil(t, toolCalls[0].Arguments)
}

func TestProcessSSE_ToolCallWithThoughtSignature(t *testing.T) {
	// Tool calls can carry thought signatures for multi-turn reasoning.
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"search","args":{"q":"test"},"id":"fc_1"},"thoughtSignature":"AAAAAAAAAA=="}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	doneEvent := events[len(events)-1]
	toolCalls := doneEvent.Message.ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "AAAAAAAAAA==", toolCalls[0].Signature)
}

func TestProcessSSE_ToolCallIDGeneration(t *testing.T) {
	// When API omits ID, provider generates one.
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{}}}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New(WithToolCallIDFunc(func() string { return "generated-id" }))
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	doneEvent := events[len(events)-1]
	toolCalls := doneEvent.Message.ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "generated-id", toolCalls[0].ID)
}

func TestProcessSSE_EmptyLines(t *testing.T) {
	sse := `
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]}}]}}

data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}}

`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	doneEvent := events[len(events)-1]
	require.NotNil(t, doneEvent.Message)
	assert.Equal(t, "hi", doneEvent.Message.Text())
}

func TestProcessSSE_DoneSentinel(t *testing.T) {
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}}
data: [DONE]
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	for _, e := range events {
		assert.NotEqual(t, ai.EventError, e.Type)
	}
}

func TestProcessSSE_CachedTokens(t *testing.T) {
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"totalTokenCount":110,"cachedContentTokenCount":30}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	doneEvent := events[len(events)-1]
	assert.Equal(t, 70, doneEvent.Message.Usage.Input) // 100 - 30
	assert.Equal(t, 10, doneEvent.Message.Usage.Output)
	assert.Equal(t, 30, doneEvent.Message.Usage.CacheRead)
}

func TestProcessSSE_ContentIndexIncrement(t *testing.T) {
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"think","thought":true}]}}]}}
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"answer"}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	for _, e := range events {
		switch e.Type {
		case ai.EventThinkStart, ai.EventThinkDelta:
			assert.Equal(t, 0, e.ContentIndex)
		case ai.EventTextStart, ai.EventTextDelta:
			assert.Equal(t, 1, e.ContentIndex)
		}
	}
}

func TestProcessSSE_MaxTokensStopReason(t *testing.T) {
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}]}}
data: {"response":{"candidates":[{"finishReason":"MAX_TOKENS"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":100,"totalTokenCount":110}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	doneEvent := events[len(events)-1]
	assert.Equal(t, ai.StopReasonLength, doneEvent.StopReason)
}

func TestProcessSSE_TextThenToolCall(t *testing.T) {
	// Model emits text first, then a tool call. Text should be closed before tool.
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Let me check "}]}}]}}
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"location":"NYC"},"id":"call_1"}}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	var eventTypes []ai.EventType
	for _, e := range events {
		eventTypes = append(eventTypes, e.Type)
	}

	// Verify TextEnd comes before ToolStart
	textEndIdx := -1
	toolStartIdx := -1
	for i, et := range eventTypes {
		switch et {
		case ai.EventTextEnd:
			textEndIdx = i
		case ai.EventToolStart:
			toolStartIdx = i
		}
	}

	assert.Greater(t, toolStartIdx, textEndIdx, "ToolStart should come after TextEnd")
	assert.Equal(t, ai.StopReasonToolUse, events[len(events)-1].StopReason)
}

func TestProcessSSE_MultipleToolCalls(t *testing.T) {
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"location":"NYC"},"id":"call_1"}},{"functionCall":{"name":"get_time","args":{"timezone":"EST"},"id":"call_2"}}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	doneEvent := events[len(events)-1]
	toolCalls := doneEvent.Message.ToolCalls()
	require.Len(t, toolCalls, 2)
	assert.Equal(t, "get_weather", toolCalls[0].Name)
	assert.Equal(t, "get_time", toolCalls[1].Name)
}

func TestProcessSSE_EmptyStream(t *testing.T) {
	// Ported from pi-mono: google-gemini-cli-empty-stream.test.ts
	// Empty stream should still emit Done with stop reason.
	sse := ``
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	require.Len(t, events, 1)
	assert.Equal(t, ai.EventDone, events[0].Type)
	assert.Equal(t, ai.StopReasonStop, events[0].StopReason)
	require.NotNil(t, events[0].Message)
	assert.Empty(t, events[0].Message.Content)
}

func TestProcessSSE_ThinkingThenToolCall(t *testing.T) {
	// Model thinks, then calls a tool. Thinking should be closed before tool.
	sse := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"I should check...","thought":true}]}}]}}
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"search","args":{"q":"weather"},"id":"fc_1"},"thoughtSignature":"sig456"}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	var eventTypes []ai.EventType
	for _, e := range events {
		eventTypes = append(eventTypes, e.Type)
	}

	thinkEndIdx := -1
	toolStartIdx := -1
	for i, et := range eventTypes {
		switch et {
		case ai.EventThinkEnd:
			thinkEndIdx = i
		case ai.EventToolStart:
			toolStartIdx = i
		}
	}

	assert.Greater(t, toolStartIdx, thinkEndIdx, "ToolStart after ThinkEnd")

	// Thinking content should have the signature from the tool call part
	doneEvent := events[len(events)-1]
	thinking, ok := ai.AsContent[ai.Thinking](doneEvent.Message.Content[0])
	require.True(t, ok)
	assert.Equal(t, "sig456", thinking.Signature)
}

func TestProcessSSE_InvalidJSON(t *testing.T) {
	// Invalid JSON lines should be skipped without error.
	sse := `data: not-json
data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]}}
data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}}
`
	p := New()
	var events []ai.Event
	push := func(e ai.Event) { events = append(events, e) }

	p.processSSE(strings.NewReader(sse), push, ai.Model{ID: "test"})

	// Should not have error events
	for _, e := range events {
		assert.NotEqual(t, ai.EventError, e.Type)
	}

	doneEvent := events[len(events)-1]
	assert.Equal(t, "ok", doneEvent.Message.Text())
}

func TestBuildGenerationConfig(t *testing.T) {
	t.Run("empty options", func(t *testing.T) {
		config := buildGenerationConfig(ai.StreamOptions{})
		assert.Nil(t, config)
	})

	t.Run("with temperature", func(t *testing.T) {
		temp := 0.7
		config := buildGenerationConfig(ai.StreamOptions{
			Temperature: &temp,
		})
		require.NotNil(t, config)
		require.NotNil(t, config.Temperature)
		assert.InDelta(t, 0.7, *config.Temperature, 0.001)
	})

	t.Run("with max tokens", func(t *testing.T) {
		maxTokens := 1000
		config := buildGenerationConfig(ai.StreamOptions{
			MaxTokens: &maxTokens,
		})
		require.NotNil(t, config)
		require.NotNil(t, config.MaxOutputTokens)
		assert.Equal(t, int32(1000), *config.MaxOutputTokens)
	})

	t.Run("with thinking level", func(t *testing.T) {
		config := buildGenerationConfig(ai.StreamOptions{
			ThinkingLevel: ai.ThinkingHigh,
		})
		require.NotNil(t, config)
		require.NotNil(t, config.ThinkingConfig)
		assert.True(t, config.ThinkingConfig.IncludeThoughts)
		assert.Equal(t, int32(24576), *config.ThinkingConfig.ThinkingBudget)
	})

	t.Run("thinking levels", func(t *testing.T) {
		tests := []struct {
			level  ai.ThinkingLevel
			budget int32
		}{
			{ai.ThinkingMinimal, 128},
			{ai.ThinkingLow, 2048},
			{ai.ThinkingMedium, 8192},
			{ai.ThinkingHigh, 24576},
			{ai.ThinkingXHigh, 32768},
		}
		for _, tt := range tests {
			t.Run(string(tt.level), func(t *testing.T) {
				config := buildGenerationConfig(ai.StreamOptions{
					ThinkingLevel: tt.level,
				})
				require.NotNil(t, config.ThinkingConfig)
				assert.Equal(t, tt.budget, *config.ThinkingConfig.ThinkingBudget)
			})
		}
	})
}
