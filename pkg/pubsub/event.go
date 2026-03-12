package pubsub

import (
	"context"
	"time"
)

// Event wraps a payload with a sequence number and timestamp,
// assigned by [Broker.Publish].
type Event[T any] struct {
	payload   T
	seq       int64
	timestamp time.Time
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
	Publish(payload T)
}

// PubSub combines both Subscriber and Publisher interfaces.
type PubSub[T any] interface {
	Subscriber[T]
	Publisher[T]
}
