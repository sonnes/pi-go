package agent

import (
	"context"
	"iter"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/pubsub"
)

// EventStream wraps agent events with dual consumption patterns:
// iterate individual events via [EventStream.Events], or await
// all new messages via [EventStream.Result].
//
// Multiple consumers can call [EventStream.Events] concurrently —
// each gets an independent subscription with replay of buffered events.
type EventStream struct {
	broker *pubsub.Broker[Event]
	msgs   []ai.Message
	err    error
	done   chan struct{}
}

// NewStream creates a stream. The producer runs in a goroutine
// and pushes events via the callback. The stream completes when
// the producer pushes an [EventAgentEnd] event.
func NewStream(fn func(push func(Event))) *EventStream {
	s := &EventStream{
		broker: pubsub.NewBroker[Event](pubsub.WithBlockingPublish()),
		done:   make(chan struct{}),
	}

	go func() {
		fn(func(e Event) {
			s.broker.Publish(e)

			if e.Type == EventAgentEnd {
				s.msgs = e.Messages
				s.err = e.Err
				close(s.done)
			}
		})
	}()

	return s
}

// Events returns an iterator over agent events.
// Each call creates an independent subscription — multiple goroutines
// can consume events concurrently. Late subscribers receive buffered
// events via replay before switching to live events.
//
// Iteration stops after [EventAgentEnd]. Cancel the context to
// unsubscribe early.
func (s *EventStream) Events(ctx context.Context) iter.Seq2[Event, error] {
	ctx, cancel := context.WithCancel(ctx)
	ch := s.broker.Subscribe(ctx, pubsub.After(0))

	return func(yield func(Event, error) bool) {
		defer cancel()
		for pe := range ch {
			event := pe.Payload()
			if !yield(event, nil) {
				return
			}
			if event.Type == EventAgentEnd {
				return
			}
		}
	}
}

// Result blocks until the agent completes and returns all new messages
// produced during the run.
func (s *EventStream) Result() ([]ai.Message, error) {
	<-s.done
	return s.msgs, s.err
}

// ErrStream returns an [EventStream] that immediately emits an error
// [EventAgentEnd].
func ErrStream(err error) *EventStream {
	return NewStream(func(push func(Event)) {
		push(Event{
			Type: EventAgentEnd,
			Err:  err,
		})
	})
}
