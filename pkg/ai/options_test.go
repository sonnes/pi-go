package ai_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestApplyOptions(t *testing.T) {
	opts := ai.ApplyOptions([]ai.Option{
		ai.WithTemperature(0.7),
		ai.WithMaxTokens(1000),
		ai.WithThinking(ai.ThinkingHigh),
		ai.WithToolChoice(ai.ToolChoiceRequired),
		ai.WithHeaders(map[string]string{"X-Custom": "val"}),
		ai.WithMetadata(map[string]any{"key": "value"}),
	})

	require.NotNil(t, opts.Temperature)
	assert.InDelta(t, 0.7, *opts.Temperature, 0.0001)

	require.NotNil(t, opts.MaxTokens)
	assert.Equal(t, 1000, *opts.MaxTokens)

	assert.Equal(t, ai.ThinkingHigh, opts.ThinkingLevel)
	assert.Equal(t, ai.ToolChoiceRequired, opts.ToolChoice)
	assert.Equal(t, "val", opts.Headers["X-Custom"])
	assert.Equal(t, "value", opts.Metadata["key"])
}

func TestSpecificToolChoice(t *testing.T) {
	tc := ai.SpecificToolChoice("my_tool")
	assert.Equal(t, ai.ToolChoice("my_tool"), tc)
}
