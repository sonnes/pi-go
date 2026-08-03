package claudecli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildArgs_MaxBudgetUSD(t *testing.T) {
	p := New(WithMaxBudgetUSD(5.25))
	args := buildArgs(
		p.cfg,
		sendArgs{prompt: "summarize this", noPersistence: true},
	)

	assert.Contains(t, args, "--max-budget-usd")
	assert.Contains(t, args, "5.25")
}
