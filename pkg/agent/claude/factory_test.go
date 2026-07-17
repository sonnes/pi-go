package claude

import (
	"testing"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_ParsesSpec(t *testing.T) {
	f := Factory()

	a, err := f("claude/sonnet")
	require.NoError(t, err)
	assert.Equal(t, "sonnet", a.(*Agent).cfg.model)
}

func TestFactory_MergesBaseAndCallOptions(t *testing.T) {
	// Base options apply first; per-call options override them.
	f := Factory(agent.WithMaxTurns(3), WithCLIPath("/base/claude"))

	a, err := f("claude/opus", agent.WithMaxTurns(7))
	require.NoError(t, err)
	assert.Equal(t, 7, a.(*Agent).cfg.maxTurns)
	assert.Equal(t, "/base/claude", a.(*Agent).cfg.cliPath)
}

func TestFactory_EmptyModel(t *testing.T) {
	f := Factory()

	_, err := f("claude/")
	assert.ErrorContains(t, err, "spec")

	_, err = f("claude")
	assert.ErrorContains(t, err, "spec")
}
