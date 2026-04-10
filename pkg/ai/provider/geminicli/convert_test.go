package geminicli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func TestConvertMessages_UserText(t *testing.T) {
	messages := []ai.Message{
		ai.UserMessage("hello"),
	}

	contents := convertMessages(messages)
	require.Len(t, contents, 1)
	assert.Equal(t, "user", contents[0].Role)
	require.Len(t, contents[0].Parts, 1)
	assert.Equal(t, "hello", contents[0].Parts[0].Text)
}

func TestConvertMessages_UserImage(t *testing.T) {
	// base64 for "test"
	b64 := "dGVzdA=="
	messages := []ai.Message{
		ai.UserImageMessage("describe", ai.Image{
			Data:     b64,
			MimeType: "image/png",
		}),
	}

	contents := convertMessages(messages)
	require.Len(t, contents, 1)
	require.Len(t, contents[0].Parts, 2)

	assert.Equal(t, "describe", contents[0].Parts[0].Text)
	assert.NotNil(t, contents[0].Parts[1].InlineData)
	assert.Equal(t, "image/png", contents[0].Parts[1].InlineData.MIMEType)
}

func TestConvertMessages_AssistantText(t *testing.T) {
	messages := []ai.Message{
		ai.AssistantMessage(ai.Text{Text: "hello back"}),
	}

	contents := convertMessages(messages)
	require.Len(t, contents, 1)
	assert.Equal(t, "model", contents[0].Role)
	require.Len(t, contents[0].Parts, 1)
	assert.Equal(t, "hello back", contents[0].Parts[0].Text)
}

func TestConvertMessages_AssistantThinking(t *testing.T) {
	messages := []ai.Message{
		ai.AssistantMessage(
			ai.Thinking{
				Thinking:  "reasoning...",
				Signature: "sig123",
			},
			ai.Text{Text: "answer"},
		),
	}

	contents := convertMessages(messages)
	require.Len(t, contents, 1)
	require.Len(t, contents[0].Parts, 2)

	thinkPart := contents[0].Parts[0]
	assert.True(t, thinkPart.Thought)
	assert.Equal(t, "reasoning...", thinkPart.Text)
	assert.Equal(t, "sig123", thinkPart.ThoughtSignature)

	textPart := contents[0].Parts[1]
	assert.Equal(t, "answer", textPart.Text)
	assert.False(t, textPart.Thought)
}

func TestConvertMessages_AssistantToolCall(t *testing.T) {
	messages := []ai.Message{
		ai.AssistantMessage(ai.ToolCall{
			ID:        "call_1",
			Name:      "get_weather",
			Arguments: map[string]any{"location": "NYC"},
		}),
	}

	contents := convertMessages(messages)
	require.Len(t, contents, 1)
	require.Len(t, contents[0].Parts, 1)

	fc := contents[0].Parts[0].FunctionCall
	require.NotNil(t, fc)
	assert.Equal(t, "get_weather", fc.Name)
	assert.Equal(t, "call_1", fc.ID)
	assert.Equal(t, "NYC", fc.Args["location"])
}

func TestConvertMessages_ToolResult(t *testing.T) {
	messages := []ai.Message{
		ai.ToolResultMessage("call_1", "get_weather", ai.Text{
			Text: `{"temp": 72}`,
		}),
	}

	contents := convertMessages(messages)
	require.Len(t, contents, 1)
	assert.Equal(t, "user", contents[0].Role)
	require.Len(t, contents[0].Parts, 1)

	fr := contents[0].Parts[0].FunctionResponse
	require.NotNil(t, fr)
	assert.Equal(t, "get_weather", fr.Name)
	assert.Equal(t, "call_1", fr.ID)
	assert.NotNil(t, fr.Response["result"])
}

func TestConvertMessages_ToolResultError(t *testing.T) {
	msg := ai.ErrorToolResultMessage("call_1", "get_weather", "not found")

	contents := convertMessages([]ai.Message{msg})
	require.Len(t, contents, 1)

	fr := contents[0].Parts[0].FunctionResponse
	require.NotNil(t, fr)
	assert.Equal(t, "not found", fr.Response["error"])
}

func TestConvertMessages_MultiTurn(t *testing.T) {
	messages := []ai.Message{
		ai.UserMessage("hi"),
		ai.AssistantMessage(ai.Text{Text: "hello"}),
		ai.UserMessage("weather?"),
		ai.AssistantMessage(ai.ToolCall{
			ID:        "c1",
			Name:      "get_weather",
			Arguments: map[string]any{"location": "NYC"},
		}),
		ai.ToolResultMessage("c1", "get_weather", ai.Text{Text: `{"temp":72}`}),
		ai.AssistantMessage(ai.Text{Text: "72F"}),
	}

	contents := convertMessages(messages)
	require.Len(t, contents, 6)

	assert.Equal(t, "user", contents[0].Role)
	assert.Equal(t, "model", contents[1].Role)
	assert.Equal(t, "user", contents[2].Role)
	assert.Equal(t, "model", contents[3].Role)
	assert.Equal(t, "user", contents[4].Role) // tool result
	assert.Equal(t, "model", contents[5].Role)
}

func TestConvertTools(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:        "get_weather",
			Description: "Get weather",
		},
	}

	result, config := convertTools(tools, ai.ToolChoiceAuto)
	require.Len(t, result, 1)
	require.Len(t, result[0].FunctionDeclarations, 1)

	decl := result[0].FunctionDeclarations[0]
	assert.Equal(t, "get_weather", decl.Name)
	assert.Equal(t, "Get weather", decl.Description)

	require.NotNil(t, config)
	assert.Equal(t, "AUTO", config.FunctionCallingConfig.Mode)
}

func TestConvertToolChoice(t *testing.T) {
	tests := []struct {
		name     string
		choice   ai.ToolChoice
		expected string
	}{
		{"auto", ai.ToolChoiceAuto, "AUTO"},
		{"none", ai.ToolChoiceNone, "NONE"},
		{"required", ai.ToolChoiceRequired, "ANY"},
		{"empty", "", "AUTO"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertToolChoice(tt.choice))
		})
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		reason   string
		expected ai.StopReason
	}{
		{"STOP", ai.StopReasonStop},
		{"MAX_TOKENS", ai.StopReasonLength},
		{"OTHER", ai.StopReasonError},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			assert.Equal(t, tt.expected, mapStopReason(tt.reason))
		})
	}
}

func TestMapUsage(t *testing.T) {
	meta := &UsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    50,
		TotalTokenCount:         150,
		CachedContentTokenCount: 20,
	}

	usage := mapUsage(meta)
	assert.Equal(t, 80, usage.Input) // 100 - 20
	assert.Equal(t, 50, usage.Output)
	assert.Equal(t, 150, usage.Total)
	assert.Equal(t, 20, usage.CacheRead)
}
