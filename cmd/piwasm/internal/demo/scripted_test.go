package demo

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestScriptedReplaysTurnsInOrder(t *testing.T) {
	s := NewScripted([]Turn{
		{Text: "first"},
		{Text: "second"},
	})

	for _, want := range []string{"first", "second"} {
		msg, err := s.StreamText(t.Context(), ai.Model{ID: "demo"}, ai.Prompt{}, ai.StreamOptions{}).Wait()
		require.NoError(t, err)
		assert.Equal(t, want, msg.Text())
	}
}

func TestScriptedRepeatsFinalTurnWhenExhausted(t *testing.T) {
	s := NewScripted([]Turn{{Text: "only"}})

	for range 3 {
		msg, err := s.StreamText(t.Context(), ai.Model{ID: "demo"}, ai.Prompt{}, ai.StreamOptions{}).Wait()
		require.NoError(t, err)
		assert.Equal(t, "only", msg.Text())
	}
}

func TestScriptedEmitsTextDeltasThatReconstructTheMessage(t *testing.T) {
	s := NewScripted([]Turn{{Text: "one two three"}})

	var got strings.Builder
	stream := s.StreamText(t.Context(), ai.Model{ID: "demo"}, ai.Prompt{}, ai.StreamOptions{})
	for e, err := range stream.Events() {
		require.NoError(t, err)
		if e.Type == ai.EventTextDelta {
			got.WriteString(e.Delta)
		}
	}

	assert.Equal(t, "one two three", got.String())
}

func TestScriptedToolTurnStopsForToolUse(t *testing.T) {
	s := NewScripted([]Turn{{
		Text: "checking",
		Tool: &ai.ToolCall{
			ID:        "call-1",
			Name:      "get_weather",
			Arguments: map[string]any{"city": "Paris"},
		},
	}})

	msg, err := s.StreamText(t.Context(), ai.Model{ID: "demo"}, ai.Prompt{}, ai.StreamOptions{}).Wait()
	require.NoError(t, err)

	assert.Equal(t, ai.StopReasonToolUse, msg.StopReason)
	calls := msg.ToolCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "get_weather", calls[0].Name)
}

func TestScriptedIsSafeForConcurrentUse(t *testing.T) {
	s := NewScripted([]Turn{{Text: "a"}, {Text: "b"}})

	done := make(chan struct{})
	for range 2 {
		go func() {
			defer close(done)
			_, _ = s.StreamText(context.Background(), ai.Model{ID: "demo"}, ai.Prompt{}, ai.StreamOptions{}).Wait()
		}()
		<-done
		done = make(chan struct{})
	}
}
