package anthropic

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func userText(s string) ai.Message {
	return ai.Message{
		Role:    ai.RoleUser,
		Content: []ai.Content{ai.Text{Text: s}},
	}
}

func toolResult(id, text string) ai.Message {
	return ai.Message{
		Role:       ai.RoleToolResult,
		ToolCallID: id,
		Content:    []ai.Content{ai.Text{Text: text}},
	}
}

func assistantText(s string) ai.Message {
	return ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.Content{ai.Text{Text: s}},
	}
}

func TestBuildParams_CacheControl_DefaultMarksSystemAndLastBlock(t *testing.T) {
	prompt := ai.Prompt{
		System:   "you are helpful",
		Messages: []ai.Message{userText("hello")},
	}
	params, _ := buildParams(ai.Model{ID: "claude"}, prompt, ai.StreamOptions{}, "")

	require.Len(t, params.System, 1)
	assert.Equal(t, "ephemeral", string(params.System[0].CacheControl.Type))
	assert.Empty(t, string(params.System[0].CacheControl.TTL))

	require.Len(t, params.Messages, 1)
	last := params.Messages[len(params.Messages)-1]
	require.Len(t, last.Content, 1)
	require.NotNil(t, last.Content[0].OfText)
	assert.Equal(t, "ephemeral", string(last.Content[0].OfText.CacheControl.Type))
	assert.Empty(t, string(last.Content[0].OfText.CacheControl.TTL))
}

func TestBuildParams_CacheControl_NoneSkipsMarkers(t *testing.T) {
	prompt := ai.Prompt{
		System:   "you are helpful",
		Messages: []ai.Message{userText("hello")},
	}
	opts := ai.StreamOptions{CacheRetention: ai.CacheRetentionNone}
	params, _ := buildParams(ai.Model{ID: "claude"}, prompt, opts, "")

	require.Len(t, params.System, 1)
	assert.Empty(t, string(params.System[0].CacheControl.Type))

	require.Len(t, params.Messages, 1)
	require.NotNil(t, params.Messages[0].Content[0].OfText)
	assert.Empty(t, string(params.Messages[0].Content[0].OfText.CacheControl.Type))
}

func TestBuildParams_CacheControl_LongOfficialURLSets1hTTL(t *testing.T) {
	prompt := ai.Prompt{
		System:   "you are helpful",
		Messages: []ai.Message{userText("hello")},
	}
	opts := ai.StreamOptions{CacheRetention: ai.CacheRetentionLong}

	for _, baseURL := range []string{"", "https://api.anthropic.com/v1/"} {
		t.Run(baseURL, func(t *testing.T) {
			params, _ := buildParams(ai.Model{ID: "claude"}, prompt, opts, baseURL)

			require.Len(t, params.System, 1)
			assert.Equal(
				t,
				anthropic.CacheControlEphemeralTTLTTL1h,
				params.System[0].CacheControl.TTL,
			)

			last := params.Messages[len(params.Messages)-1]
			require.NotNil(t, last.Content[0].OfText)
			assert.Equal(
				t,
				anthropic.CacheControlEphemeralTTLTTL1h,
				last.Content[0].OfText.CacheControl.TTL,
			)
		})
	}
}

func TestBuildParams_CacheControl_LongProxyOmitsTTL(t *testing.T) {
	prompt := ai.Prompt{
		System:   "you are helpful",
		Messages: []ai.Message{userText("hello")},
	}
	opts := ai.StreamOptions{CacheRetention: ai.CacheRetentionLong}
	params, _ := buildParams(ai.Model{ID: "claude"}, prompt, opts, "https://proxy.example/anthropic")

	require.Len(t, params.System, 1)
	assert.Equal(t, "ephemeral", string(params.System[0].CacheControl.Type))
	assert.Empty(t, string(params.System[0].CacheControl.TTL))

	last := params.Messages[len(params.Messages)-1]
	require.NotNil(t, last.Content[0].OfText)
	assert.Equal(t, "ephemeral", string(last.Content[0].OfText.CacheControl.Type))
	assert.Empty(t, string(last.Content[0].OfText.CacheControl.TTL))
}

func TestBuildParams_CacheControl_LastBlockIsToolResult(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{
			userText("use the tool"),
			assistantText("ok"),
			toolResult("call-1", "tool output"),
		},
	}
	params, _ := buildParams(ai.Model{ID: "claude"}, prompt, ai.StreamOptions{}, "")

	require.NotEmpty(t, params.Messages)
	last := params.Messages[len(params.Messages)-1]
	require.NotEmpty(t, last.Content)
	block := last.Content[len(last.Content)-1]
	require.NotNil(t, block.OfToolResult, "last block should be a tool_result")
	assert.Equal(t, "ephemeral", string(block.OfToolResult.CacheControl.Type))
}

func TestBuildParams_CacheControl_NoSystemNoPanic(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{userText("hi")},
	}
	params, _ := buildParams(ai.Model{ID: "claude"}, prompt, ai.StreamOptions{}, "")

	assert.Empty(t, params.System)
	require.Len(t, params.Messages, 1)
	require.NotNil(t, params.Messages[0].Content[0].OfText)
	assert.Equal(t, "ephemeral", string(params.Messages[0].Content[0].OfText.CacheControl.Type))
}
