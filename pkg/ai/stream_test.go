package ai_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestEventStream_Wait(t *testing.T) {
	expected := &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.Content{ai.Text{Text: "hello"}},
	}

	stream := ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
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
		return expected, nil
	})

	msg, err := stream.Wait()
	require.NoError(t, err)
	assert.Equal(t, expected, msg)
}

func TestEventStream_Error(t *testing.T) {
	stream := ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
		return nil, assert.AnError
	})

	_, err := stream.Wait()
	assert.ErrorIs(t, err, assert.AnError)
}

func TestEventStream_Events(t *testing.T) {
	stream := ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		push(ai.Event{Type: ai.EventTextStart})
		push(ai.Event{Type: ai.EventTextDelta, Delta: "hi"})
		push(ai.Event{Type: ai.EventTextEnd, Content: "hi"})
		return &ai.Message{Role: ai.RoleAssistant}, nil
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
	}, types)
}

func TestEventStream_EventsYieldProducerError(t *testing.T) {
	stream := ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		push(ai.Event{Type: ai.EventTextDelta, Delta: "partial"})
		return nil, assert.AnError
	})

	var types []ai.EventType
	var streamErr error
	for e, err := range stream.Events() {
		if err != nil {
			streamErr = err
			continue
		}
		types = append(types, e.Type)
	}

	assert.Equal(t, []ai.EventType{ai.EventTextDelta}, types)
	assert.ErrorIs(t, streamErr, assert.AnError)
}

func TestEventStream_WaitAfterAbandonedEvents(t *testing.T) {
	expected := &ai.Message{Role: ai.RoleAssistant}

	stream := ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		// Push well past the channel buffer to prove an abandoned
		// consumer cannot block the producer.
		for range 100 {
			push(ai.Event{Type: ai.EventTextDelta, Delta: "x"})
		}
		return expected, nil
	})

	for range stream.Events() {
		break
	}

	msg, err := stream.Wait()
	require.NoError(t, err)
	assert.Equal(t, expected, msg)
}

func TestStreamText_UnregisteredProvider(t *testing.T) {
	ai.ClearProviders()
	defer ai.ClearProviders()

	model := ai.Model{Provider: "nonexistent"}
	_, err := ai.StreamText(
		context.Background(),
		model,
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	).Wait()

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
		ai.Model{Provider: "fake"},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	)
	require.NoError(t, err)
	assert.Equal(t, expected, msg)
}
