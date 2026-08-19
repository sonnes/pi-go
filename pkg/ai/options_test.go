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

func TestWithCacheRetention(t *testing.T) {
	opts := ai.ApplyOptions([]ai.Option{
		ai.WithCacheRetention(ai.CacheRetentionLong),
	})
	assert.Equal(t, ai.CacheRetentionLong, opts.CacheRetention)
}

func TestWithSessionID(t *testing.T) {
	opts := ai.ApplyOptions([]ai.Option{
		ai.WithSessionID("session-123"),
	})
	assert.Equal(t, "session-123", opts.SessionID)
}

func TestResolveCacheRetention(t *testing.T) {
	tests := []struct {
		name  string
		input ai.CacheRetention
		want  ai.CacheRetention
	}{
		{
			name:  "default empty resolves to short",
			input: ai.CacheRetentionDefault,
			want:  ai.CacheRetentionShort,
		},
		{
			name:  "none stays none",
			input: ai.CacheRetentionNone,
			want:  ai.CacheRetentionNone,
		},
		{
			name:  "short stays short",
			input: ai.CacheRetentionShort,
			want:  ai.CacheRetentionShort,
		},
		{
			name:  "long stays long",
			input: ai.CacheRetentionLong,
			want:  ai.CacheRetentionLong,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ai.ResolveCacheRetention(tt.input))
		})
	}
}

func TestResolveThinkingLevel(t *testing.T) {
	model := ai.Model{
		ThinkingLevels: []ai.ThinkingLevel{
			ai.ThinkingOff,
			ai.ThinkingLow,
			ai.ThinkingHigh,
		},
	}
	tests := []struct {
		name         string
		requested    ai.ThinkingLevel
		wantLevel    ai.ThinkingLevel
		wantDegraded bool
	}{
		{
			name:         "empty request resolves to off",
			requested:    "",
			wantLevel:    ai.ThinkingOff,
			wantDegraded: false,
		},
		{
			name:         "supported level stays selected",
			requested:    ai.ThinkingHigh,
			wantLevel:    ai.ThinkingHigh,
			wantDegraded: false,
		},
		{
			name:         "unsupported positive level maps down",
			requested:    ai.ThinkingMedium,
			wantLevel:    ai.ThinkingLow,
			wantDegraded: true,
		},
		{
			name:         "unsupported deepest level maps down",
			requested:    ai.ThinkingXHigh,
			wantLevel:    ai.ThinkingHigh,
			wantDegraded: true,
		},
		{
			name:         "max maps down to high",
			requested:    ai.ThinkingMax,
			wantLevel:    ai.ThinkingHigh,
			wantDegraded: true,
		},
		{
			name:         "unknown level maps off",
			requested:    ai.ThinkingLevel("adaptive"),
			wantLevel:    ai.ThinkingOff,
			wantDegraded: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, degraded := ai.ResolveThinkingLevel(model, tt.requested)
			assert.Equal(t, tt.wantLevel, got)
			assert.Equal(t, tt.wantDegraded, degraded)
		})
	}
}

func TestResolveThinkingLevelWithoutModelSupport(t *testing.T) {
	got, degraded := ai.ResolveThinkingLevel(
		ai.Model{},
		ai.ThinkingHigh,
	)

	assert.Equal(t, ai.ThinkingOff, got)
	assert.True(t, degraded)
}

func TestResolveThinkingLevelMaxOutranksXHigh(t *testing.T) {
	model := ai.Model{
		ThinkingLevels: []ai.ThinkingLevel{
			ai.ThinkingHigh,
			ai.ThinkingXHigh,
			ai.ThinkingMax,
		},
	}

	got, degraded := ai.ResolveThinkingLevel(model, ai.ThinkingMax)
	assert.Equal(t, ai.ThinkingMax, got)
	assert.False(t, degraded)

	capped := ai.Model{
		ThinkingLevels: []ai.ThinkingLevel{
			ai.ThinkingLow,
			ai.ThinkingXHigh,
		},
	}

	got, degraded = ai.ResolveThinkingLevel(capped, ai.ThinkingMax)
	assert.Equal(t, ai.ThinkingXHigh, got)
	assert.True(t, degraded)
}

func TestEffectiveThinkingLevel(t *testing.T) {
	full := ai.Model{
		ThinkingLevels: []ai.ThinkingLevel{
			ai.ThinkingLow,
			ai.ThinkingMedium,
			ai.ThinkingHigh,
		},
		DefaultThinkingLevel: ai.ThinkingHigh,
	}
	noDefault := ai.Model{
		ThinkingLevels: []ai.ThinkingLevel{ai.ThinkingLow},
	}
	noLevels := ai.Model{}

	tests := []struct {
		name      string
		model     ai.Model
		requested ai.ThinkingLevel
		want      ai.ThinkingLevel
	}{
		{
			name:      "explicit off stays off",
			model:     full,
			requested: ai.ThinkingOff,
			want:      ai.ThinkingOff,
		},
		{
			name:      "unset takes the model default",
			model:     full,
			requested: "",
			want:      ai.ThinkingHigh,
		},
		{
			name:      "unset without a default is off",
			model:     noDefault,
			requested: "",
			want:      ai.ThinkingOff,
		},
		{
			name:      "unset on a model without levels is off",
			model:     noLevels,
			requested: "",
			want:      ai.ThinkingOff,
		},
		{
			name:      "request above the ceiling maps down",
			model:     full,
			requested: ai.ThinkingMax,
			want:      ai.ThinkingHigh,
		},
		{
			name:      "supported request passes through",
			model:     full,
			requested: ai.ThinkingLow,
			want:      ai.ThinkingLow,
		},
		{
			name:      "model without levels does not clamp the request",
			model:     noLevels,
			requested: ai.ThinkingHigh,
			want:      ai.ThinkingHigh,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ai.EffectiveThinkingLevel(tt.model, tt.requested)
			assert.Equal(t, tt.want, got)
		})
	}
}
