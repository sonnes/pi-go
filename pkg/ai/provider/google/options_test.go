package google

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"google.golang.org/genai"
)

func TestUsesThinkingLevel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{
			name:  "Gemini 2 uses a numeric budget",
			model: "gemini-2.5-flash",
			want:  false,
		},
		{
			name:  "Gemini 3 uses a level",
			model: "gemini-3.5-flash",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := usesThinkingLevel(ai.Model{ID: tt.model})
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyOptions_Gemini3UsesThinkingLevel(t *testing.T) {
	temperature := 0.2
	config := &genai.GenerateContentConfig{}

	applyOptions(
		config,
		ai.Model{ID: "gemini-3.5-flash"},
		ai.StreamOptions{
			Temperature:   &temperature,
			ThinkingLevel: ai.ThinkingXHigh,
		},
	)

	require.NotNil(t, config.ThinkingConfig)
	assert.Equal(t, genai.ThinkingLevelHigh, config.ThinkingConfig.ThinkingLevel)
	assert.Nil(t, config.ThinkingConfig.ThinkingBudget)
	assert.Nil(t, config.Temperature)
}
