package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEmitsPackageModelCatalog(t *testing.T) {
	provider := mdProvider{Models: map[string]mdModel{
		"model-1": {ID: "model-1", Name: "Model 1"},
	}}

	src, count := generate(
		target{key: "fake", pkg: "fake"},
		provider,
	)
	require.Equal(t, 1, count)

	generated := string(src)
	assert.Contains(t, generated, "func Models() []ai.Model")
	assert.NotContains(t, generated, "func (p *Provider) Models()")
	assert.True(t, strings.Contains(generated, "func (p *Provider) LanguageModel"))
}

// reasoningOption builds a models.dev reasoning_options entry.
func reasoningOption(kind string, values ...string) mdModel {
	m := mdModel{ID: "m", Name: "M"}
	m.ReasoningOptions = append(m.ReasoningOptions, struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	}{Type: kind, Values: values})
	return m
}

func TestModelLiteralEmitsThinkingLevels(t *testing.T) {
	tests := []struct {
		name    string
		model   mdModel
		want    []string
		notWant []string
	}{
		{
			name:  "effort values map to levels and default high",
			model: reasoningOption("effort", "low", "medium", "high", "xhigh", "max"),
			want: []string{
				"ThinkingLevels: []ai.ThinkingLevel{" +
					"ai.ThinkingLow, ai.ThinkingMedium, ai.ThinkingHigh, " +
					"ai.ThinkingXHigh, ai.ThinkingMax}",
				"DefaultThinkingLevel: ai.ThinkingHigh",
			},
		},
		{
			name:  "without high the default is the deepest level",
			model: reasoningOption("effort", "medium"),
			want: []string{
				"ThinkingLevels: []ai.ThinkingLevel{ai.ThinkingMedium}",
				"DefaultThinkingLevel: ai.ThinkingMedium",
			},
		},
		{
			name:    "budget_tokens carries no levels",
			model:   reasoningOption("budget_tokens"),
			notWant: []string{"ThinkingLevels", "DefaultThinkingLevel"},
		},
		{
			name:  "unknown values are skipped",
			model: reasoningOption("effort", "medium", "bananas"),
			want: []string{
				"ThinkingLevels: []ai.ThinkingLevel{ai.ThinkingMedium}",
				"DefaultThinkingLevel: ai.ThinkingMedium",
			},
			notWant: []string{"bananas"},
		},
		{
			name:  "none maps to off",
			model: reasoningOption("effort", "none", "high"),
			want: []string{
				"ThinkingLevels: []ai.ThinkingLevel{ai.ThinkingOff, ai.ThinkingHigh}",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelLiteral(tt.model)
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tt.notWant {
				assert.NotContains(t, got, notWant)
			}
		})
	}
}
