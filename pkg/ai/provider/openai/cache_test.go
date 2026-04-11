package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func TestBuildParams_PromptCacheKey_SetFromSessionID(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: []ai.Content{ai.Text{Text: "hi"}}},
		},
	}
	opts := ai.StreamOptions{SessionID: "session-42"}
	params := buildParams(ai.Model{ID: "gpt-4o"}, prompt, opts)

	require.True(t, params.PromptCacheKey.Valid())
	assert.Equal(t, "session-42", params.PromptCacheKey.Value)
}

func TestBuildParams_PromptCacheKey_OmittedWhenSessionEmpty(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: []ai.Content{ai.Text{Text: "hi"}}},
		},
	}
	params := buildParams(ai.Model{ID: "gpt-4o"}, prompt, ai.StreamOptions{})

	assert.False(t, params.PromptCacheKey.Valid())
}

func TestBuildParams_PromptCacheKey_SuppressedWhenCacheNone(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: []ai.Content{ai.Text{Text: "hi"}}},
		},
	}
	opts := ai.StreamOptions{
		SessionID:      "session-42",
		CacheRetention: ai.CacheRetentionNone,
	}
	params := buildParams(ai.Model{ID: "gpt-4o"}, prompt, opts)

	assert.False(t, params.PromptCacheKey.Valid())
}
