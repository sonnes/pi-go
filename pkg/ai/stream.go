package ai

import (
	"context"
	"fmt"
	"iter"
	"sync"
)

// EventStream wraps streaming events with dual consumption patterns:
// iterate individual events via Events(), or await the final message via Result().
type EventStream struct {
	ch     chan Event
	done   chan struct{}
	result *Message
	err    error
	once   sync.Once
}

// NewEventStream creates a stream. The producer runs in a goroutine
// and pushes events via the callback.
func NewEventStream(fn func(push func(Event))) *EventStream {
	s := &EventStream{
		ch:   make(chan Event, 16),
		done: make(chan struct{}),
	}
	go func() {
		defer close(s.ch)
		fn(func(e Event) {
			s.ch <- e
		})
	}()
	return s
}

// Events returns an iterator over streaming events.
// Iteration stops after an EventDone or EventError event.
func (s *EventStream) Events() iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		defer s.once.Do(func() { close(s.done) })

		for e := range s.ch {
			switch e.Type {
			case EventDone:
				s.result = e.Message
				yield(e, nil)
				return
			case EventError:
				s.err = e.Err
				s.result = e.Message
				yield(e, e.Err)
				return
			default:
				if !yield(e, nil) {
					return
				}
			}
		}
	}
}

// Result blocks until the stream completes and returns the final message.
func (s *EventStream) Result() (*Message, error) {
	// If Events() hasn't been called, drain the stream.
	s.once.Do(func() {
		defer close(s.done)
		for e := range s.ch {
			switch e.Type {
			case EventDone:
				s.result = e.Message
				return
			case EventError:
				s.err = e.Err
				s.result = e.Message
				return
			}
		}
	})
	<-s.done

	if s.err != nil {
		return s.result, s.err
	}
	return s.result, nil
}

// StreamText streams a text response from the model.
func StreamText(ctx context.Context, model Model, p Prompt, opts ...Option) *EventStream {
	prov, ok := GetProvider(model.API)
	if !ok {
		return errStream(fmt.Errorf("ai: no provider registered for API %q", model.API))
	}
	o := ApplyOptions(opts)
	return prov.StreamText(ctx, model, p, o)
}

// GenerateText generates a text response synchronously.
// Convenience wrapper around StreamText(...).Result().
func GenerateText(ctx context.Context, model Model, p Prompt, opts ...Option) (*Message, error) {
	return StreamText(ctx, model, p, opts...).Result()
}

// errStream returns an EventStream that immediately emits an error event.
func errStream(err error) *EventStream {
	return NewEventStream(func(push func(Event)) {
		push(Event{
			Type: EventError,
			Err:  err,
		})
	})
}
