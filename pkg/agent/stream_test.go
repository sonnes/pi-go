package agent

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStream_EmitsEvents(t *testing.T) {
	stream := NewStream(func(push func(Event)) {
		push(Event{Type: EventAgentStart})
		push(Event{Type: EventTurnStart})
		push(Event{Type: EventTurnEnd})
		push(Event{
			Type:     EventAgentEnd,
			Messages: []ai.Message{{Role: ai.RoleAssistant}},
		})
	})

	ctx := t.Context()
	var received []EventType
	for event, err := range stream.Events(ctx) {
		require.NoError(t, err)
		received = append(received, event.Type)
	}

	assert.Equal(t, []EventType{
		EventAgentStart,
		EventTurnStart,
		EventTurnEnd,
		EventAgentEnd,
	}, received)
}

func TestNewStream_Result(t *testing.T) {
	want := []ai.Message{
		{Role: ai.RoleAssistant},
		{Role: ai.RoleAssistant},
	}

	stream := NewStream(func(push func(Event)) {
		push(Event{Type: EventAgentStart})
		push(Event{
			Type:     EventAgentEnd,
			Messages: want,
		})
	})

	msgs, err := stream.Result()
	require.NoError(t, err)
	assert.Equal(t, want, msgs)
}

func TestNewStream_Result_Error(t *testing.T) {
	stream := NewStream(func(push func(Event)) {
		push(Event{Type: EventAgentStart})
		push(Event{
			Type: EventAgentEnd,
			Err:  errors.New("model error"),
		})
	})

	msgs, err := stream.Result()
	assert.ErrorContains(t, err, "model error")
	assert.Nil(t, msgs)
}

func TestNewStream_MultipleSubscribers(t *testing.T) {
	stream := NewStream(func(push func(Event)) {
		push(Event{Type: EventAgentStart})
		push(Event{Type: EventTurnStart})
		push(Event{Type: EventTurnEnd})
		push(Event{
			Type:     EventAgentEnd,
			Messages: []ai.Message{{Role: ai.RoleAssistant}},
		})
	})

	ctx := t.Context()

	var wg sync.WaitGroup
	results := make([][]EventType, 2)

	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for event, err := range stream.Events(ctx) {
				require.NoError(t, err)
				results[idx] = append(results[idx], event.Type)
			}
		}(i)
	}

	wg.Wait()

	expected := []EventType{
		EventAgentStart,
		EventTurnStart,
		EventTurnEnd,
		EventAgentEnd,
	}
	assert.Equal(t, expected, results[0])
	assert.Equal(t, expected, results[1])
}

func TestNewStream_LateSubscriber(t *testing.T) {
	started := make(chan struct{})
	proceed := make(chan struct{})

	stream := NewStream(func(push func(Event)) {
		push(Event{Type: EventAgentStart})
		push(Event{Type: EventTurnStart})
		// Signal that first events are published
		close(started)
		// Wait for late subscriber to attach
		<-proceed
		push(Event{Type: EventTurnEnd})
		push(Event{
			Type:     EventAgentEnd,
			Messages: []ai.Message{{Role: ai.RoleAssistant}},
		})
	})

	ctx := t.Context()

	// First subscriber gets everything live
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range stream.Events(ctx) {
		}
	}()

	// Wait for first events to be published
	<-started

	// Late subscriber — should replay from ring buffer
	var lateEvents []EventType
	wg.Add(1)
	go func() {
		defer wg.Done()
		for event, _ := range stream.Events(ctx) {
			lateEvents = append(lateEvents, event.Type)
		}
	}()

	// Allow a moment for the late subscriber to attach
	time.Sleep(20 * time.Millisecond)
	close(proceed)

	wg.Wait()

	// Late subscriber should have all events via replay + live
	assert.Equal(t, []EventType{
		EventAgentStart,
		EventTurnStart,
		EventTurnEnd,
		EventAgentEnd,
	}, lateEvents)
}

func TestErrStream(t *testing.T) {
	stream := ErrStream(errors.New("connection failed"))

	msgs, err := stream.Result()
	assert.ErrorContains(t, err, "connection failed")
	assert.Nil(t, msgs)
}

func TestErrStream_Events(t *testing.T) {
	stream := ErrStream(errors.New("connection failed"))

	ctx := t.Context()
	var received []Event
	for event, err := range stream.Events(ctx) {
		require.NoError(t, err)
		received = append(received, event)
	}

	require.Len(t, received, 1)
	assert.Equal(t, EventAgentEnd, received[0].Type)
	assert.ErrorContains(t, received[0].Err, "connection failed")
}
