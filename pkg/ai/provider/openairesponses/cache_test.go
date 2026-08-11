package openairesponses

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
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
	params := buildParams(ai.Model{ID: "gpt-4o"}, prompt, opts, DialectOpenAI)

	require.True(t, params.PromptCacheKey.Valid())
	assert.Equal(t, "session-42", params.PromptCacheKey.Value)
}

func TestBuildParams_PromptCacheKey_OmittedWhenSessionEmpty(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: []ai.Content{ai.Text{Text: "hi"}}},
		},
	}
	params := buildParams(ai.Model{ID: "gpt-4o"}, prompt, ai.StreamOptions{}, DialectOpenAI)

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
	params := buildParams(ai.Model{ID: "gpt-4o"}, prompt, opts, DialectOpenAI)

	assert.False(t, params.PromptCacheKey.Valid())
}

func TestBuildParams_LongCacheRetention(t *testing.T) {
	prompt := ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}}
	params := buildParams(
		ai.Model{ID: "gpt-5.6"},
		prompt,
		ai.StreamOptions{CacheRetention: ai.CacheRetentionLong},
		DialectOpenAI,
	)

	assert.Equal(
		t,
		responses.ResponseNewParamsPromptCacheRetention24h,
		params.PromptCacheRetention,
	)
}

// TestBuildParams_CodexAlwaysSetsInstructions makes sure that the request
// body holds a non-empty `instructions` field. The Codex backend requires
// it. Without it, chatgpt.com/backend-api/codex/responses returns
// `{"detail":"Instructions are required"}`. Under DialectCodex this holds
// even when the caller gives no system prompt.
func TestBuildParams_CodexAlwaysSetsInstructions(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: []ai.Content{ai.Text{Text: "hi"}}},
		},
	}
	params := buildParams(ai.Model{ID: "gpt-5.4"}, prompt, ai.StreamOptions{}, DialectCodex)

	require.True(t, params.Instructions.Valid())
	assert.NotEmpty(t, params.Instructions.Value)
}

// TestBuildParams_CodexPreservesProvidedSystem makes sure that DialectCodex
// keeps a caller-supplied system prompt. The Codex fallback does not replace
// it.
func TestBuildParams_CodexPreservesProvidedSystem(t *testing.T) {
	prompt := ai.Prompt{
		System: "you are a precise reviewer",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: []ai.Content{ai.Text{Text: "hi"}}},
		},
	}
	params := buildParams(ai.Model{ID: "gpt-5.4"}, prompt, ai.StreamOptions{}, DialectCodex)

	require.True(t, params.Instructions.Valid())
	assert.Equal(t, "you are a precise reviewer", params.Instructions.Value)
}

// TestBuildParams_OpenAIDialectInstructionsOptional documents that the
// default OpenAI dialect omits Instructions when the caller gives no system
// prompt. Only DialectCodex sets a value in that case.
func TestBuildParams_OpenAIDialectInstructionsOptional(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: []ai.Content{ai.Text{Text: "hi"}}},
		},
	}
	params := buildParams(ai.Model{ID: "gpt-4o"}, prompt, ai.StreamOptions{}, DialectOpenAI)

	assert.False(t, params.Instructions.Valid())
}
