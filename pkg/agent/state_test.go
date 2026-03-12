package agent

import (
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestState_ZeroValue(t *testing.T) {
	var s State

	assert.False(t, s.IsStreaming())
	assert.Nil(t, s.StreamMessage())
	assert.Empty(t, s.PendingToolCalls())
	assert.NoError(t, s.Err())
}

func TestState_Getters(t *testing.T) {
	msg := NewLLMMessage(ai.UserMessage("hello"))
	streamMsg := ai.AssistantMessage(ai.Text{Text: "partial"})
	pending := map[string]struct{}{"call-1": {}, "call-2": {}}
	testErr := assert.AnError

	s := State{
		isStreaming:      true,
		streamMessage:    &streamMsg,
		pendingToolCalls: pending,
		err:              testErr,
		messages:         []Message{msg},
	}

	assert.True(t, s.IsStreaming())
	require.NotNil(t, s.StreamMessage())
	assert.Equal(t, streamMsg, *s.StreamMessage())
	assert.Equal(t, pending, s.PendingToolCalls())
	assert.Equal(t, testErr, s.Err())
	assert.Equal(t, []Message{msg}, s.Messages())
}

func TestState_PendingToolCalls_ReturnsCopy(t *testing.T) {
	s := State{
		pendingToolCalls: map[string]struct{}{"call-1": {}},
	}

	got := s.PendingToolCalls()
	got["call-2"] = struct{}{}

	assert.Len(t, s.PendingToolCalls(), 1, "original should not be modified")
}

func TestState_Messages_ReturnsCopy(t *testing.T) {
	msg := NewLLMMessage(ai.UserMessage("hello"))
	s := State{
		messages: []Message{msg},
	}

	got := s.Messages()
	got = append(got, NewLLMMessage(ai.UserMessage("extra")))

	assert.Len(t, s.Messages(), 1, "original should not be modified")
}

func TestState_LLMMessages(t *testing.T) {
	s := State{
		messages: []Message{
			NewLLMMessage(ai.UserMessage("hello")),
			CustomMessage{Kind: "status"},
			NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "hi"})),
		},
	}

	got := s.LLMMessages()
	require.Len(t, got, 2)
	assert.Equal(t, ai.RoleUser, got[0].Role)
	assert.Equal(t, ai.RoleAssistant, got[1].Role)
}
