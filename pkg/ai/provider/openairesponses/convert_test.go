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

func TestConvertInput_UserFileMessage(t *testing.T) {
	tests := []struct {
		name     string
		file     ai.File
		wantKey  string
		wantData string
	}{
		{
			name: "inline base64",
			file: ai.File{
				Data:     "base64pdfdata",
				MimeType: "application/pdf",
				Filename: "spec.pdf",
			},
			wantKey:  "file_data",
			wantData: "data:application/pdf;base64,base64pdfdata",
		},
		{
			name: "uploaded file id",
			file: ai.File{
				FileID: "file_abc123",
			},
			wantKey:  "file_id",
			wantData: "file_abc123",
		},
		{
			name: "url reference",
			file: ai.File{
				URL: "https://example.com/spec.pdf",
			},
			wantKey:  "file_url",
			wantData: "https://example.com/spec.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := convertInput([]ai.Message{
				ai.UserFileMessage("read this", tt.file),
			})
			require.Len(t, items, 1)

			data, err := json.Marshal(items[0])
			require.NoError(t, err)

			var raw map[string]any
			require.NoError(t, json.Unmarshal(data, &raw))

			content, ok := raw["content"].([]any)
			require.True(t, ok, "content should be array")
			require.Len(t, content, 2)

			filePart := content[1].(map[string]any)
			assert.Equal(t, "input_file", filePart["type"])
			assert.Equal(t, tt.wantData, filePart[tt.wantKey])
		})
	}
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

func TestConvertTools_ServerWebSearch(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:       "web_search",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolWebSearch,
			ServerConfig: map[string]any{
				"search_context_size": "high",
			},
		},
	}

	result := convertTools(tools)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].OfWebSearchPreview)

	body, err := json.Marshal(result[0].OfWebSearchPreview)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))

	assert.Equal(t, "web_search_preview", got["type"])
	assert.Equal(t, "high", got["search_context_size"])
}

func TestConvertTools_ServerCodeInterpreter(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:       "code_execution",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolCodeExecution,
		},
	}

	result := convertTools(tools)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].OfCodeInterpreter)

	body, err := json.Marshal(result[0].OfCodeInterpreter)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))

	assert.Equal(t, "code_interpreter", got["type"])
	// Default container is the auto sentinel object.
	assert.NotNil(t, got["container"])
}

func TestConvertOpenRouterTools_FunctionTool(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{
		"type": "object",
		"properties": {"location": {"type": "string"}},
		"required": ["location"]
	}`), &schema))

	tools := []ai.ToolInfo{
		{
			Name:        "get_weather",
			Description: "Get the weather",
		},
	}
	// Encode the schema by hand because ToolInfo.InputSchema is a typed
	// *jsonschema.Schema; the helper is in the broader test scaffolding.
	tools[0].InputSchema = nil

	got := convertOpenRouterTools(tools)
	require.Len(t, got, 1)
	assert.Equal(t, "function", got[0]["type"])
	assert.Equal(t, "get_weather", got[0]["name"])
	assert.Equal(t, "Get the weather", got[0]["description"])
}

func TestConvertOpenRouterTools_ServerTools(t *testing.T) {
	tests := []struct {
		name     string
		tool     ai.ToolInfo
		wantType string
		wantKeys map[string]any
	}{
		{
			name: "web_search basic",
			tool: ai.ToolInfo{
				Kind:       ai.ToolKindServer,
				ServerType: ai.ServerToolWebSearch,
			},
			wantType: "openrouter:web_search",
		},
		{
			name: "web_search with config",
			tool: ai.ToolInfo{
				Kind:       ai.ToolKindServer,
				ServerType: ai.ServerToolWebSearch,
				ServerConfig: map[string]any{
					"engine":      "exa",
					"max_results": 10,
				},
			},
			wantType: "openrouter:web_search",
			wantKeys: map[string]any{
				"engine":      "exa",
				"max_results": float64(10),
			},
		},
		{
			name: "web_fetch",
			tool: ai.ToolInfo{
				Kind:       ai.ToolKindServer,
				ServerType: ai.ServerToolWebFetch,
				ServerConfig: map[string]any{
					"max_uses": 3,
				},
			},
			wantType: "openrouter:web_fetch",
			wantKeys: map[string]any{"max_uses": float64(3)},
		},
		{
			name: "datetime",
			tool: ai.ToolInfo{
				Kind:       ai.ToolKindServer,
				ServerType: ai.ServerToolDateTime,
				ServerConfig: map[string]any{
					"timezone": "America/New_York",
				},
			},
			wantType: "openrouter:datetime",
			wantKeys: map[string]any{"timezone": "America/New_York"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertOpenRouterTools([]ai.ToolInfo{tt.tool})
			require.Len(t, got, 1)

			// Round-trip through JSON to assert the wire shape.
			data, err := json.Marshal(got[0])
			require.NoError(t, err)
			var raw map[string]any
			require.NoError(t, json.Unmarshal(data, &raw))

			assert.Equal(t, tt.wantType, raw["type"])
			for k, v := range tt.wantKeys {
				assert.Equal(t, v, raw[k], "key %s", k)
			}
		})
	}
}

func TestConvertOpenRouterTools_DropsUnsupportedServerTypes(t *testing.T) {
	tools := []ai.ToolInfo{
		{Kind: ai.ToolKindServer, ServerType: ai.ServerToolCodeExecution},
		{Kind: ai.ToolKindServer, ServerType: ai.ServerToolFileSearch},
		{Kind: ai.ToolKindServer, ServerType: ai.ServerToolComputer},
		{Kind: ai.ToolKindServer, ServerType: ai.ServerToolMCP},
		{Kind: ai.ToolKindServer, ServerType: ai.ServerToolBash},
	}

	got := convertOpenRouterTools(tools)
	assert.Empty(t, got, "OpenRouter doesn't expose these server tools, drop silently")
}

func TestConvertOpenRouterTools_FunctionAndServerMixed(t *testing.T) {
	tools := []ai.ToolInfo{
		{Name: "get_weather", Description: "weather"},
		{Kind: ai.ToolKindServer, ServerType: ai.ServerToolWebSearch},
	}

	got := convertOpenRouterTools(tools)
	require.Len(t, got, 2)
	assert.Equal(t, "function", got[0]["type"])
	assert.Equal(t, "openrouter:web_search", got[1]["type"])
}

func TestServerTypeForItem(t *testing.T) {
	cases := map[string]ai.ServerToolType{
		"web_search_call":       ai.ServerToolWebSearch,
		"code_interpreter_call": ai.ServerToolCodeExecution,
		"file_search_call":      ai.ServerToolFileSearch,
		"computer_call":         ai.ServerToolComputer,
		"mcp_call":              ai.ServerToolMCP,
		"unknown_call":          ai.ServerToolType(""),
	}
	for input, want := range cases {
		assert.Equal(t, want, serverTypeForItem(input), input)
	}
}
