package agent_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
)

func TestErrStream_FailsImmediately(t *testing.T) {
	sentinel := errors.New("no backend")

	msgs, err := agent.ErrStream(sentinel).Wait()
	require.ErrorIs(t, err, sentinel)
	assert.Empty(t, msgs)
}
