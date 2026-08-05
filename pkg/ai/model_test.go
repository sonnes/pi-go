package ai_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

// TestUsageJSON pins the usage wire format: snake_case like the rest of
// the package, every field omitted when zero, and the cost breakdown
// absent entirely when nothing was priced. Callers persist ai.Usage
// directly, so these keys are the format.
func TestUsageJSON(t *testing.T) {
	t.Run("keys are snake_case", func(t *testing.T) {
		usage := ai.Usage{
			Input:       10,
			Output:      20,
			CacheRead:   30,
			CacheWrite:  40,
			Reasoning:   50,
			InputAudio:  60,
			OutputAudio: 70,
			Cost:        ai.UsageCost{Input: 0.1, Output: 0.2, CacheRead: 0.3},
		}

		data, err := json.Marshal(usage)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"input": 10,
			"output": 20,
			"cache_read": 30,
			"cache_write": 40,
			"reasoning": 50,
			"input_audio": 60,
			"output_audio": 70,
			"cost": {"input": 0.1, "output": 0.2, "cache_read": 0.3}
		}`, string(data))
	})

	t.Run("zero usage is an empty object", func(t *testing.T) {
		data, err := json.Marshal(ai.Usage{})
		require.NoError(t, err)
		assert.JSONEq(t, `{}`, string(data))
	})

	t.Run("zero cost is omitted", func(t *testing.T) {
		data, err := json.Marshal(ai.Usage{Input: 5})
		require.NoError(t, err)
		assert.JSONEq(t, `{"input": 5}`, string(data))
	})

	t.Run("round-trips", func(t *testing.T) {
		usage := ai.Usage{
			Input:  7,
			Output: 3,
			Cost:   ai.UsageCost{Input: 0.007, Output: 0.003},
		}

		data, err := json.Marshal(usage)
		require.NoError(t, err)

		var decoded ai.Usage
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, usage, decoded)
	})
}

// TestUsageAdd covers the accumulator callers use to sum usage across
// turns and runs.
func TestUsageAdd(t *testing.T) {
	a := ai.Usage{
		Input:       1,
		Output:      2,
		CacheRead:   3,
		CacheWrite:  4,
		Reasoning:   5,
		InputAudio:  6,
		OutputAudio: 7,
		Cost: ai.UsageCost{
			Input:       0.1,
			Output:      0.2,
			CacheRead:   0.3,
			CacheWrite:  0.4,
			Reasoning:   0.5,
			InputAudio:  0.6,
			OutputAudio: 0.7,
		},
	}

	got := a.Add(a)

	assert.Equal(t, 2, got.Input)
	assert.Equal(t, 4, got.Output)
	assert.Equal(t, 6, got.CacheRead)
	assert.Equal(t, 8, got.CacheWrite)
	assert.Equal(t, 10, got.Reasoning)
	assert.Equal(t, 12, got.InputAudio)
	assert.Equal(t, 14, got.OutputAudio)

	assert.InDelta(t, 0.2, got.Cost.Input, 0.0001)
	assert.InDelta(t, 0.4, got.Cost.Output, 0.0001)
	assert.InDelta(t, 0.6, got.Cost.CacheRead, 0.0001)
	assert.InDelta(t, 0.8, got.Cost.CacheWrite, 0.0001)
	assert.InDelta(t, 1.0, got.Cost.Reasoning, 0.0001)
	assert.InDelta(t, 1.2, got.Cost.InputAudio, 0.0001)
	assert.InDelta(t, 1.4, got.Cost.OutputAudio, 0.0001)

	t.Run("zero is the identity", func(t *testing.T) {
		assert.Equal(t, a, a.Add(ai.Usage{}))
		assert.Equal(t, a, ai.Usage{}.Add(a))
	})
}
