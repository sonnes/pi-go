package openairesponses

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

// TestBuildParams_CodexAlwaysSetsInstructions verifies the Codex backend's
// hard requirement: the request body MUST carry a non-empty `instructions`
// field, or chatgpt.com/backend-api/codex/responses returns
// `{"detail":"Instructions are required"}`. Under DialectCodex this must hold
// even when the caller did not provide a system prompt.
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

// TestBuildParams_CodexPreservesProvidedSystem verifies that a caller-provided
// system prompt is preserved (not replaced by the Codex fallback) under
// DialectCodex.
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
// default OpenAI dialect still omits Instructions when no system prompt is
// given — only DialectCodex forces it.
func TestBuildParams_OpenAIDialectInstructionsOptional(t *testing.T) {
	prompt := ai.Prompt{
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: []ai.Content{ai.Text{Text: "hi"}}},
		},
	}
	params := buildParams(ai.Model{ID: "gpt-4o"}, prompt, ai.StreamOptions{}, DialectOpenAI)

	assert.False(t, params.Instructions.Valid())
}
