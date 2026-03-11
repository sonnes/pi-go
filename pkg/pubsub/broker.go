package pubsub

import (
	"context"
	"sync"
	"time"

	"github.com/sonnes/pi-go/pkg/buffer"
)

// Broker is a generic pub/sub broker that manages subscriptions and
// distributes events to all subscribers. It maintains a ring buffer of
// recent events, allowing new subscribers to replay from a given
// sequence number.
type Broker[T any] struct {
	subs       map[chan Event[T]]struct{}
	mu         sync.Mutex
	done       chan struct{}
	bufferSize int
	ring       *buffer.Ring[Event[T]]
}

// NewBroker creates a new Broker with the given options.
//
// Example:
//
//	broker := pubsub.NewBroker[MyPayload]()
//	defer broker.Shutdown()
//
//	// With custom options:
//	broker := pubsub.NewBroker[MyPayload](
//	    pubsub.WithBufferSize(128),
//	)
func NewBroker[T any](opts ...Option) *Broker[T] {
	o := applyOptions(opts)
	return &Broker[T]{
		subs:       make(map[chan Event[T]]struct{}),
		done:       make(chan struct{}),
		bufferSize: o.bufferSize,
		ring:       buffer.NewRing[Event[T]](o.maxEvents),
	}
}

// Subscribe creates a new subscription that receives events until the
// context is canceled or the broker is shut down.
//
// The returned channel is closed when the subscription ends. Subscribers
// should range over the channel to receive events.
//
// Use [After] to replay buffered events from a sequence number:
//
//	events := broker.Subscribe(ctx, pubsub.After(lastSeq))
func (b *Broker[T]) Subscribe(
	ctx context.Context,
	opts ...SubscribeOption,
) <-chan Event[T] {
	subOpts := applySubscribeOptions(opts)

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if broker is already shut down
	select {
	case <-b.done:
		ch := make(chan Event[T])
		close(ch)
		return ch
	default:
	}

	// Collect replay events to size the channel appropriately
	var replay []buffer.Entry[Event[T]]
	if subOpts.hasAfter {
		replay = b.ring.After(subOpts.after)
	}

	chanSize := max(b.bufferSize, len(replay))

	sub := make(chan Event[T], chanSize)

	for _, entry := range replay {
		event := entry.Value
		event.seq = entry.Seq
		sub <- event
	}

	b.subs[sub] = struct{}{}

	// Start goroutine to handle unsubscription when context is done
	go func() {
		<-ctx.Done()

		b.mu.Lock()
		defer b.mu.Unlock()

		// Check if broker was shut down (which already cleaned up)
		select {
		case <-b.done:
			return
		default:
		}

		delete(b.subs, sub)
		close(sub)
	}()

	return sub
}

// Publish sends an event to all current subscribers.
//
// Publishing is non-blocking: if a subscriber's channel is full, the event
// is dropped for that subscriber. This prevents slow subscribers from
// blocking the publisher or other subscribers.
func (b *Broker[T]) Publish(eventType EventType, payload T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if broker is shut down
	select {
	case <-b.done:
		return
	default:
	}

	event := newEvent(eventType, payload)
	event.timestamp = time.Now()

	// Store in ring buffer; seq is assigned by the ring
	event.seq = b.ring.Write(event)

	for sub := range b.subs {
		select {
		case sub <- event:
			// Event sent successfully
		default:
			// Channel full, subscriber is slow - skip this event
		}
	}
}

// Shutdown gracefully shuts down the broker.
//
// All subscriber channels are closed, and subsequent calls to Subscribe
// will return immediately-closed channels. Shutdown is safe to call
// multiple times.
func (b *Broker[T]) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.done:
		return
	default:
		close(b.done)
	}

	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
}

// SubscriberCount returns the current number of active subscribers.
func (b *Broker[T]) SubscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// IsClosed returns true if the broker has been shut down.
func (b *Broker[T]) IsClosed() bool {
	select {
	case <-b.done:
		return true
	default:
		return false
	}
}
