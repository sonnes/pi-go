package ai_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestCalculateCost(t *testing.T) {
	model := ai.Model{
		Cost: ai.Cost{
			Input:      3.0,
			Output:     15.0,
			CacheRead:  0.3,
			CacheWrite: 3.75,
		},
	}
	usage := ai.Usage{
		Input:      1_000_000,
		Output:     500_000,
		CacheRead:  200_000,
		CacheWrite: 100_000,
	}

	cost := ai.CalculateCost(model, usage)

	assert.InDelta(t, 3.0, cost.Input, 0.0001)
	assert.InDelta(t, 7.5, cost.Output, 0.0001)
	assert.InDelta(t, 0.06, cost.CacheRead, 0.0001)
	assert.InDelta(t, 0.375, cost.CacheWrite, 0.0001)
	assert.InDelta(t, 10.935, cost.Total, 0.0001)
}

func TestCalculateCostExtendedRates(t *testing.T) {
	model := ai.Model{
		Cost: ai.Cost{
			Reasoning:   30.0,
			InputAudio:  40.0,
			OutputAudio: 80.0,
		},
	}
	usage := ai.Usage{
		Reasoning:   100_000,
		InputAudio:  50_000,
		OutputAudio: 25_000,
	}

	cost := ai.CalculateCost(model, usage)

	assert.InDelta(t, 3.0, cost.Reasoning, 0.0001)
	assert.InDelta(t, 2.0, cost.InputAudio, 0.0001)
	assert.InDelta(t, 2.0, cost.OutputAudio, 0.0001)
	assert.InDelta(t, 7.0, cost.Total, 0.0001)
}
