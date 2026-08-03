package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sonnes/pi-go/pkg/agent"
)

func TestOptions(t *testing.T) {
	// A package minting options into the shared currency often has
	// several to contribute but only one Option to return.
	bundled := agent.Options(
		agent.WithMaxTurns(4),
		agent.WithSystemPrompt("be brief"),
	)

	cfg := agent.ApplyOptions(bundled, agent.WithMaxTurns(9))

	assert.Equal(t, "be brief", cfg.SystemPrompt)
	assert.Equal(t, 9, cfg.MaxTurns, "a bundled option applies in place, so later options still win")
}

func TestOptionsEmpty(t *testing.T) {
	cfg := agent.ApplyOptions(agent.Options())
	assert.Zero(t, cfg.MaxTurns)
}

func TestOptionsNested(t *testing.T) {
	cfg := agent.ApplyOptions(agent.Options(
		agent.Options(agent.WithMaxTurns(2)),
		agent.WithSystemPrompt("nested"),
	))

	assert.Equal(t, 2, cfg.MaxTurns)
	assert.Equal(t, "nested", cfg.SystemPrompt)
}
