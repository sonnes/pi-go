package openairesponses

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func TestConvertInput_UserTextMessage(t *testing.T) {
	messages := []ai.Message{
		ai.UserMessage("hello"),
	}

	items := convertInput(messages)
	require.Len(t, items, 1)

	data, err := json.Marshal(items[0])
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "user", raw["role"])
	assert.Equal(t, "hello", raw["content"])
}

func TestConvertInput_UserImageMessage(t *testing.T) {
	messages := []ai.Message{
		ai.UserImageMessage("describe this", ai.Image{
			Data:     "base64data",
			MimeType: "image/png",
		}),
	}

	items := convertInput(messages)
	require.Len(t, items, 1)

	data, err := json.Marshal(items[0])
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "user", raw["role"])

	content, ok := raw["content"].([]any)
	require.True(t, ok, "content should be array")
	require.Len(t, content, 2)

	textPart := content[0].(map[string]any)
	assert.Equal(t, "input_text", textPart["type"])
	assert.Equal(t, "describe this", textPart["text"])

	imagePart := content[1].(map[string]any)
	assert.Equal(t, "input_image", imagePart["type"])
	assert.Equal(t, "data:image/png;base64,base64data", imagePart["image_url"])
}

func TestConvertInput_AssistantTextMessage(t *testing.T) {
	messages := []ai.Message{
		ai.AssistantMessage(ai.Text{Text: "I can help"}),
	}

	items := convertInput(messages)
	require.Len(t, items, 1)

	data, err := json.Marshal(items[0])
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "assistant", raw["role"])
	assert.Equal(t, "I can help", raw["content"])
}

func TestConvertInput_AssistantToolCall(t *testing.T) {
	messages := []ai.Message{
		ai.AssistantMessage(
			ai.Text{Text: "Let me check"},
			ai.ToolCall{
				ID:        "call_123",
				Name:      "get_weather",
				Arguments: map[string]any{"location": "NYC"},
			},
		),
	}

	items := convertInput(messages)
	require.Len(t, items, 2, "text and tool call should be separate items")

	// First item: assistant text message
	data, err := json.Marshal(items[0])
	require.NoError(t, err)
	var textRaw map[string]any
	require.NoError(t, json.Unmarshal(data, &textRaw))
	assert.Equal(t, "assistant", textRaw["role"])
	assert.Equal(t, "Let me check", textRaw["content"])

	// Second item: function call
	data, err = json.Marshal(items[1])
	require.NoError(t, err)
	var callRaw map[string]any
	require.NoError(t, json.Unmarshal(data, &callRaw))
	assert.Equal(t, "function_call", callRaw["type"])
	assert.Equal(t, "call_123", callRaw["call_id"])
	assert.Equal(t, "get_weather", callRaw["name"])
}

func TestConvertInput_AssistantThinking(t *testing.T) {
	messages := []ai.Message{
		ai.AssistantMessage(
			ai.Thinking{
				Thinking:  "Let me think...",
				Signature: "reasoning_abc",
			},
			ai.Text{Text: "The answer is 42"},
		),
	}

	items := convertInput(messages)
	require.Len(t, items, 2, "reasoning and text should be separate items")

	// First item: text message (prepended)
	data, err := json.Marshal(items[0])
	require.NoError(t, err)
	var textRaw map[string]any
	require.NoError(t, json.Unmarshal(data, &textRaw))
	assert.Equal(t, "assistant", textRaw["role"])

	// Second item: reasoning
	data, err = json.Marshal(items[1])
	require.NoError(t, err)
	var reasonRaw map[string]any
	require.NoError(t, json.Unmarshal(data, &reasonRaw))
	assert.Equal(t, "reasoning", reasonRaw["type"])
	assert.Equal(t, "reasoning_abc", reasonRaw["id"])
}

func TestConvertInput_ToolResult(t *testing.T) {
	messages := []ai.Message{
		ai.ToolResultMessage("call_123", "get_weather", ai.Text{
			Text: `{"temp": 72}`,
		}),
	}

	items := convertInput(messages)
	require.Len(t, items, 1)

	data, err := json.Marshal(items[0])
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "function_call_output", raw["type"])
	assert.Equal(t, "call_123", raw["call_id"])
	assert.Equal(t, `{"temp": 72}`, raw["output"])
}

func TestConvertInput_MultiTurnConversation(t *testing.T) {
	messages := []ai.Message{
		ai.UserMessage("What's the weather in NYC?"),
		ai.AssistantMessage(
			ai.ToolCall{
				ID:        "call_1",
				Name:      "get_weather",
				Arguments: map[string]any{"location": "NYC"},
			},
		),
		ai.ToolResultMessage("call_1", "get_weather", ai.Text{
			Text: `{"temp": 72}`,
		}),
		ai.AssistantMessage(ai.Text{Text: "It's 72F in NYC"}),
		ai.UserMessage("Thanks!"),
	}

	items := convertInput(messages)
	require.Len(t, items, 5)
}

func TestConvertTools(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
		},
	}

	result := convertTools(tools)
	require.Len(t, result, 1)

	data, err := json.Marshal(result[0])
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "function", raw["type"])
	assert.Equal(t, "get_weather", raw["name"])
	assert.Equal(t, "Get weather for a location", raw["description"])
}

func TestConvertToolChoice(t *testing.T) {
	tests := []struct {
		name     string
		choice   ai.ToolChoice
		expected string
	}{
		{"auto", ai.ToolChoiceAuto, "auto"},
		{"none", ai.ToolChoiceNone, "none"},
		{"required", ai.ToolChoiceRequired, "required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToolChoice(tt.choice)
			data, err := json.Marshal(result)
			require.NoError(t, err)

			var raw string
			require.NoError(t, json.Unmarshal(data, &raw))
			assert.Equal(t, tt.expected, raw)
		})
	}

	t.Run("specific tool", func(t *testing.T) {
		result := convertToolChoice(ai.SpecificToolChoice("get_weather"))
		data, err := json.Marshal(result)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))
		assert.Equal(t, "function", raw["type"])
		assert.Equal(t, "get_weather", raw["name"])
	})
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		status   responses.ResponseStatus
		expected ai.StopReason
	}{
		{responses.ResponseStatusCompleted, ai.StopReasonStop},
		{responses.ResponseStatusIncomplete, ai.StopReasonLength},
		{responses.ResponseStatusFailed, ai.StopReasonError},
		{responses.ResponseStatusCancelled, ai.StopReasonError},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.expected, mapStopReason(tt.status))
		})
	}
}

func TestMapThinkingLevel(t *testing.T) {
	tests := []struct {
		level    ai.ThinkingLevel
		expected shared.ReasoningEffort
	}{
		{ai.ThinkingLow, shared.ReasoningEffortLow},
		{ai.ThinkingMedium, shared.ReasoningEffortMedium},
		{ai.ThinkingHigh, shared.ReasoningEffortHigh},
		{ai.ThinkingXHigh, shared.ReasoningEffortHigh},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			assert.Equal(t, tt.expected, mapThinkingLevel(tt.level))
		})
	}
}
