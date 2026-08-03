package anthropic

import "testing"

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func TestUsesAdaptiveThinking(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{
			name:  "Claude 4.6 keeps legacy thinking",
			model: "claude-sonnet-4-6",
			want:  false,
		},
		{
			name:  "Claude 4.5 keeps legacy thinking",
			model: "claude-sonnet-4-5",
			want:  false,
		},
		{
			name:  "Claude 4.7 uses adaptive thinking",
			model: "claude-sonnet-4-7",
			want:  true,
		},
		{
			name:  "Claude 4.8 uses adaptive thinking",
			model: "claude-opus-4-8",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := usesAdaptiveThinking(ai.Model{ID: tt.model})
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildParams_AdaptiveThinking(t *testing.T) {
	maxTokens := 4096
	params, _ := buildParams(
		ai.Model{ID: "claude-sonnet-4-7"},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("think")}},
		ai.StreamOptions{
			MaxTokens:     &maxTokens,
			ThinkingLevel: ai.ThinkingHigh,
		},
		"",
	)

	require.NotNil(t, params.Thinking.OfAdaptive)
	assert.Nil(t, params.Thinking.OfEnabled)
	assert.Equal(t, int64(maxTokens), params.MaxTokens)
	assert.Equal(t, "high", string(params.OutputConfig.Effort))
}

func TestBuildObjectParams_AdaptiveThinkingUsesNativeSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}

	params := buildObjectParams(
		ai.Model{ID: "claude-sonnet-4-7"},
		schema,
		2048,
	)

	assert.Empty(t, params.Tools)
	assert.Nil(t, params.ToolChoice.OfTool)
	assert.Equal(t, schema, params.OutputConfig.Format.Schema)
}
