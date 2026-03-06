package ai_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestEventStream_Result(t *testing.T) {
	expected := &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.Content{ai.Text{Text: "hello"}},
	}

	stream := ai.NewEventStream(func(push func(ai.Event)) {
		push(ai.Event{
			Type:         ai.EventTextStart,
			ContentIndex: 0,
		})
		push(ai.Event{
			Type:  ai.EventTextDelta,
			Delta: "hello",
		})
		push(ai.Event{
			Type:    ai.EventTextEnd,
			Content: "hello",
		})
		push(ai.Event{
			Type:    ai.EventDone,
			Message: expected,
		})
	})

	msg, err := stream.Result()
	require.NoError(t, err)
	assert.Equal(t, expected, msg)
}

func TestEventStream_Error(t *testing.T) {
	stream := ai.NewEventStream(func(push func(ai.Event)) {
		push(ai.Event{
			Type: ai.EventError,
			Err:  assert.AnError,
		})
	})

	_, err := stream.Result()
	assert.ErrorIs(t, err, assert.AnError)
}

func TestEventStream_Events(t *testing.T) {
	stream := ai.NewEventStream(func(push func(ai.Event)) {
		push(ai.Event{Type: ai.EventTextStart})
		push(ai.Event{Type: ai.EventTextDelta, Delta: "hi"})
		push(ai.Event{Type: ai.EventTextEnd, Content: "hi"})
		push(ai.Event{
			Type:    ai.EventDone,
			Message: &ai.Message{Role: ai.RoleAssistant},
		})
	})

	var types []ai.EventType
	for e, err := range stream.Events() {
		require.NoError(t, err)
		types = append(types, e.Type)
	}

	assert.Equal(t, []ai.EventType{
		ai.EventTextStart,
		ai.EventTextDelta,
		ai.EventTextEnd,
		ai.EventDone,
	}, types)
}

func TestStreamText_UnregisteredProvider(t *testing.T) {
	ai.ClearProviders()
	defer ai.ClearProviders()

	model := ai.Model{API: "nonexistent"}
	_, err := ai.StreamText(
		context.Background(),
		model,
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	).Result()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no provider registered")
}

func TestGenerateText_WithFakeProvider(t *testing.T) {
	ai.ClearProviders()
	defer ai.ClearProviders()

	expected := &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.Content{ai.Text{Text: "response"}},
	}

	p := &fakeProvider{
		api:     "fake",
		message: expected,
	}
	ai.RegisterProvider("fake", p)

	msg, err := ai.GenerateText(
		context.Background(),
		ai.Model{API: "fake"},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	)
	require.NoError(t, err)
	assert.Equal(t, expected, msg)
}
