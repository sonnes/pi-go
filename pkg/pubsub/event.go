package pubsub

import (
	"context"
	"time"
)

// EventType identifies the type of event. Publishers define their own
// event types as constants or variables of this type.
//
// Example:
//
//	const (
//	    UserCreated EventType = "user.created"
//	    UserUpdated EventType = "user.updated"
//	)
type EventType string

// Event represents an immutable event with a type, typed payload,
// sequence number, and timestamp.
type Event[T any] struct {
	eventType EventType
	payload   T
	seq       int64
	timestamp time.Time
}

// newEvent creates a new Event with the given type and payload.
// The seq and timestamp fields are set by [Broker.Publish].
func newEvent[T any](eventType EventType, payload T) Event[T] {
	return Event[T]{
		eventType: eventType,
		payload:   payload,
	}
}

// Type returns the event type.
func (e Event[T]) Type() EventType {
	return e.eventType
}

// Payload returns the event payload.
func (e Event[T]) Payload() T {
	return e.payload
}

// Seq returns the event's sequence number, assigned by the broker
// on publish. Sequence numbers are monotonically increasing.
func (e Event[T]) Seq() int64 {
	return e.seq
}

// Timestamp returns when the event was published.
func (e Event[T]) Timestamp() time.Time {
	return e.timestamp
}

// SubscribeOption configures a subscription.
type SubscribeOption func(*subscribeOptions)

type subscribeOptions struct {
	after    int64
	hasAfter bool
}

// After configures a subscription to replay buffered events with
// sequence numbers greater than seq before streaming live events.
func After(seq int64) SubscribeOption {
	return func(o *subscribeOptions) {
		o.after = seq
		o.hasAfter = true
	}
}

func applySubscribeOptions(opts []SubscribeOption) *subscribeOptions {
	o := &subscribeOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Subscriber can subscribe to events of type T.
type Subscriber[T any] interface {
	Subscribe(ctx context.Context, opts ...SubscribeOption) <-chan Event[T]
}

// Publisher can publish events of type T.
type Publisher[T any] interface {
	Publish(eventType EventType, payload T)
}

// PubSub combines both Subscriber and Publisher interfaces.
type PubSub[T any] interface {
	Subscriber[T]
	Publisher[T]
}
