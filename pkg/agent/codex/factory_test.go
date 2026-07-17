package codex

import (
	"testing"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_ParsesSpec(t *testing.T) {
	f := Factory()

	a, err := f("codex/gpt-5")
	require.NoError(t, err)
	assert.Equal(t, "gpt-5", a.(*Agent).cfg.model)
}

func TestFactory_MergesBaseAndCallOptions(t *testing.T) {
	f := Factory(agent.WithMaxTurns(3))

	a, err := f("codex/gpt-5", agent.WithMaxTurns(7))
	require.NoError(t, err)
	assert.Equal(t, 7, a.(*Agent).cfg.maxTurns)
}

func TestFactory_EmptyModel(t *testing.T) {
	f := Factory()

	_, err := f("codex/")
	assert.ErrorContains(t, err, "spec")
}
