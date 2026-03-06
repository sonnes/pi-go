package ai_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestMessageConstructors(t *testing.T) {
	t.Run("UserMessage", func(t *testing.T) {
		msg := ai.UserMessage("hello")
		assert.Equal(t, ai.RoleUser, msg.Role)
		require.Len(t, msg.Content, 1)
		text, ok := ai.AsContent[ai.Text](msg.Content[0])
		require.True(t, ok)
		assert.Equal(t, "hello", text.Text)
		assert.False(t, msg.Timestamp.IsZero())
	})

	t.Run("UserImageMessage", func(t *testing.T) {
		img := ai.Image{Data: "abc", MimeType: "image/png"}
		msg := ai.UserImageMessage("caption", img)
		assert.Equal(t, ai.RoleUser, msg.Role)
		require.Len(t, msg.Content, 2)

		text, ok := ai.AsContent[ai.Text](msg.Content[0])
		require.True(t, ok)
		assert.Equal(t, "caption", text.Text)

		gotImg, ok := ai.AsContent[ai.Image](msg.Content[1])
		require.True(t, ok)
		assert.Equal(t, "abc", gotImg.Data)
	})

	t.Run("AssistantMessage", func(t *testing.T) {
		msg := ai.AssistantMessage(ai.Text{Text: "hi"})
		assert.Equal(t, ai.RoleAssistant, msg.Role)
		require.Len(t, msg.Content, 1)
	})

	t.Run("ToolResultMessage", func(t *testing.T) {
		msg := ai.ToolResultMessage("call-1", "my_tool", ai.Text{Text: "result"})
		assert.Equal(t, ai.RoleToolResult, msg.Role)
		assert.Equal(t, "call-1", msg.ToolCallID)
		assert.Equal(t, "my_tool", msg.ToolName)
		assert.False(t, msg.IsError)
	})

	t.Run("ErrorToolResultMessage", func(t *testing.T) {
		msg := ai.ErrorToolResultMessage("call-1", "my_tool", "something broke")
		assert.Equal(t, ai.RoleToolResult, msg.Role)
		assert.True(t, msg.IsError)
	})
}

func TestMessageJSONRoundTrip(t *testing.T) {
	original := ai.AssistantMessage(
		ai.Thinking{Thinking: "let me think", Signature: "sig1"},
		ai.Text{Text: "hello world", Signature: "sig2"},
		ai.ToolCall{
			ID:        "tc-1",
			Name:      "get_weather",
			Arguments: map[string]any{"location": "Paris"},
			Signature: "sig3",
		},
	)
	original.API = "test-api"
	original.Provider = "test-provider"
	original.Model = "test-model"
	original.StopReason = ai.StopReasonToolUse
	original.Usage = ai.Usage{
		Input:  100,
		Output: 50,
		Total:  150,
		Cost: ai.UsageCost{
			Input:  0.001,
			Output: 0.002,
			Total:  0.003,
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ai.Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.Role, decoded.Role)
	assert.Equal(t, original.API, decoded.API)
	assert.Equal(t, original.Provider, decoded.Provider)
	assert.Equal(t, original.Model, decoded.Model)
	assert.Equal(t, original.StopReason, decoded.StopReason)
	assert.Equal(t, original.Usage.Input, decoded.Usage.Input)
	assert.Equal(t, original.Usage.Output, decoded.Usage.Output)
	assert.Equal(t, original.Usage.Total, decoded.Usage.Total)
	assert.InDelta(t, original.Usage.Cost.Total, decoded.Usage.Cost.Total, 0.0001)

	require.Len(t, decoded.Content, 3)

	thinking, ok := ai.AsContent[ai.Thinking](decoded.Content[0])
	require.True(t, ok)
	assert.Equal(t, "let me think", thinking.Thinking)
	assert.Equal(t, "sig1", thinking.Signature)

	text, ok := ai.AsContent[ai.Text](decoded.Content[1])
	require.True(t, ok)
	assert.Equal(t, "hello world", text.Text)

	tc, ok := ai.AsContent[ai.ToolCall](decoded.Content[2])
	require.True(t, ok)
	assert.Equal(t, "tc-1", tc.ID)
	assert.Equal(t, "get_weather", tc.Name)
	assert.Equal(t, "Paris", tc.Arguments["location"])
}
