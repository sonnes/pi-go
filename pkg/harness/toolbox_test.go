package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolbox(t *testing.T) {
	h := newTestHarness(
		t,
		&mockProvider{},
		WithTools(noopTool("read"), noopTool("write"), noopTool("bash")),
	)

	all := h.baseline.tools.list()
	require.Len(t, all, 3)
	assert.Equal(t, "read", all[0].Info().Name, "registration order is preserved")
}
