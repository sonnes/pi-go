package pubsub

import (
	"context"
	"sync"
	"time"

	"github.com/sonnes/pi-go/pkg/buffer"
)

// subscriber bundles a subscription's delivery channel with the coordination
// primitives that let the broker close it without racing in-flight publishes.
//
//   - done is closed first to signal "no more sends". In-flight publishes
//     observe this via select and bail out without touching ch.
//   - wg counts publishes that have committed to sending to this subscriber
//     (Add happens under the broker lock). The closer waits on wg before
//     closing ch, guaranteeing no goroutine ever sends on a closed channel.
type subscriber[T any] struct {
	ch   chan Event[T]
	done chan struct{}
	wg   sync.WaitGroup
}

// Broker is a generic pub/sub broker that manages subscriptions and
// distributes events to all subscribers. It maintains a ring buffer of
// recent events, allowing new subscribers to replay from a given
// sequence number.
type Broker[T any] struct {
	subs       map[*subscriber[T]]struct{}
	mu         sync.Mutex
	done       chan struct{}
	bufferSize int
	blocking   bool
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
		subs:       make(map[*subscriber[T]]struct{}),
		done:       make(chan struct{}),
		bufferSize: o.bufferSize,
		blocking:   o.blocking,
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

	sub := &subscriber[T]{
		ch:   make(chan Event[T], chanSize),
		done: make(chan struct{}),
	}

	for _, entry := range replay {
		event := entry.Value
		event.seq = entry.Seq
		sub.ch <- event
	}

	b.subs[sub] = struct{}{}

	// Unsubscribe on context cancel. Coordinate with in-flight publishes:
	// remove the sub from the map and close sub.done under b.mu (so no new
	// publish can register an Add for this sub), then wait for any publish
	// that already registered before closing sub.ch.
	go func() {
		<-ctx.Done()

		b.mu.Lock()
		if _, stillActive := b.subs[sub]; !stillActive {
			// Shutdown already took over; it will close sub.ch.
			b.mu.Unlock()
			return
		}
		delete(b.subs, sub)
		close(sub.done)
		b.mu.Unlock()

		sub.wg.Wait()
		close(sub.ch)
	}()

	return sub.ch
}

// Publish sends an event to all current subscribers.
//
// Publishing is non-blocking by default: if a subscriber's channel is full,
// the event is dropped for that subscriber. Use [WithBlockingPublish] to
// block until all subscribers receive the event.
func (b *Broker[T]) Publish(payload T) {
	b.mu.Lock()

	// Check if broker is shut down
	select {
	case <-b.done:
		b.mu.Unlock()
		return
	default:
	}

	event := Event[T]{payload: payload}
	event.timestamp = time.Now()

	// Store in ring buffer; seq is assigned by the ring
	event.seq = b.ring.Write(event)

	if !b.blocking {
		for sub := range b.subs {
			select {
			case sub.ch <- event:
			default:
			}
		}
		b.mu.Unlock()
		return
	}

	// Blocking mode: snapshot subscribers and bump each one's in-flight
	// counter while we still hold b.mu. Closers (Subscribe's cancel
	// goroutine and Shutdown) also acquire b.mu before closing sub.done,
	// so either we register our Add before the close and the closer waits
	// on wg, or the closer has already removed the sub and we skip it —
	// in both cases no goroutine ever sends on a closed channel.
	subs := make([]*subscriber[T], 0, len(b.subs))
	for sub := range b.subs {
		sub.wg.Add(1)
		subs = append(subs, sub)
	}
	b.mu.Unlock()

	for i, sub := range subs {
		if brokerDone := b.sendBlocking(sub, event); brokerDone {
			// Release wg counters we bumped for subs we didn't send to.
			for _, rest := range subs[i+1:] {
				rest.wg.Done()
			}
			return
		}
	}
}

// sendBlocking sends event to sub, blocking until the send succeeds, the
// subscriber is torn down, or the broker shuts down. Returns true only if
// the broker shut down. The caller must have bumped sub.wg.Add(1) before
// calling; this function always calls sub.wg.Done when it returns.
func (b *Broker[T]) sendBlocking(sub *subscriber[T], event Event[T]) (brokerDone bool) {
	defer sub.wg.Done()
	select {
	case sub.ch <- event:
	case <-sub.done:
		// Subscriber is torn down; the closer is waiting on wg to close ch.
	case <-b.done:
		brokerDone = true
	}
	return
}

// Shutdown gracefully shuts down the broker.
//
// All subscriber channels are closed, and subsequent calls to Subscribe
// will return immediately-closed channels. Shutdown is safe to call
// multiple times.
func (b *Broker[T]) Shutdown() {
	b.mu.Lock()

	select {
	case <-b.done:
		b.mu.Unlock()
		return
	default:
		close(b.done)
	}

	// Drain the map and signal each sub to bail under the lock, then
	// release the lock before waiting on wg so new publishes blocked on
	// b.mu can unblock, observe b.done, and return without deadlocking.
	subs := make([]*subscriber[T], 0, len(b.subs))
	for sub := range b.subs {
		subs = append(subs, sub)
		delete(b.subs, sub)
		close(sub.done)
	}
	b.mu.Unlock()

	for _, sub := range subs {
		sub.wg.Wait()
		close(sub.ch)
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
