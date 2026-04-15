package pubsub

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBroker_DefaultOptions(t *testing.T) {
	broker := NewBroker[string]()
	defer broker.Shutdown()

	assert.NotNil(t, broker)
	assert.Equal(t, 0, broker.SubscriberCount())
	assert.False(t, broker.IsClosed())
}

func TestNewBroker_WithOptions(t *testing.T) {
	broker := NewBroker[string](
		WithBufferSize(128),
		WithMaxEvents(500),
	)
	defer broker.Shutdown()

	assert.NotNil(t, broker)
	assert.Equal(t, 128, broker.bufferSize)
	assert.Equal(t, 500, broker.ring.Cap())
}

func TestBroker_Subscribe(t *testing.T) {
	broker := NewBroker[string]()
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx)
	require.NotNil(t, events)
	assert.Equal(t, 1, broker.SubscriberCount())
}

func TestBroker_Subscribe_MultipleSubscribers(t *testing.T) {
	broker := NewBroker[string]()
	defer broker.Shutdown()

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	broker.Subscribe(ctx1)
	broker.Subscribe(ctx2)

	assert.Equal(t, 2, broker.SubscriberCount())
}

func TestBroker_Publish(t *testing.T) {
	broker := NewBroker[string]()
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx)

	broker.Publish("test-payload")

	select {
	case event := <-events:
		assert.Equal(t, "test-payload", event.Payload())
		assert.Equal(t, int64(1), event.Seq())
		assert.False(t, event.Timestamp().IsZero())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestBroker_Publish_ToMultipleSubscribers(t *testing.T) {
	broker := NewBroker[string]()
	defer broker.Shutdown()

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	events1 := broker.Subscribe(ctx1)
	events2 := broker.Subscribe(ctx2)

	broker.Publish("shared-payload")

	// Both subscribers should receive the event
	for _, events := range []<-chan Event[string]{events1, events2} {
		select {
		case event := <-events:
			assert.Equal(t, "shared-payload", event.Payload())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestBroker_Unsubscribe_OnContextCancel(t *testing.T) {
	broker := NewBroker[string]()
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	events := broker.Subscribe(ctx)

	assert.Equal(t, 1, broker.SubscriberCount())

	cancel()

	// Wait for unsubscription goroutine to complete
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 0, broker.SubscriberCount())

	// Channel should be closed
	_, ok := <-events
	assert.False(t, ok, "channel should be closed")
}

func TestBroker_Shutdown(t *testing.T) {
	broker := NewBroker[string]()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx)
	assert.Equal(t, 1, broker.SubscriberCount())

	broker.Shutdown()

	assert.True(t, broker.IsClosed())
	assert.Equal(t, 0, broker.SubscriberCount())

	// Channel should be closed
	_, ok := <-events
	assert.False(t, ok, "channel should be closed after shutdown")
}

func TestBroker_Shutdown_Idempotent(t *testing.T) {
	broker := NewBroker[string]()

	// Multiple shutdown calls should not panic
	broker.Shutdown()
	broker.Shutdown()
	broker.Shutdown()

	assert.True(t, broker.IsClosed())
}

func TestBroker_Subscribe_AfterShutdown(t *testing.T) {
	broker := NewBroker[string]()
	broker.Shutdown()

	ctx := context.Background()
	events := broker.Subscribe(ctx)

	// Should return closed channel
	_, ok := <-events
	assert.False(t, ok, "channel should be immediately closed")
}

func TestBroker_Publish_AfterShutdown(t *testing.T) {
	broker := NewBroker[string]()
	broker.Shutdown()

	// Should not panic
	broker.Publish("ignored")
}

func TestBroker_Publish_NonBlocking(t *testing.T) {
	// Use small buffer to test non-blocking behavior
	broker := NewBroker[string](WithBufferSize(1))
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	broker.Subscribe(ctx)

	// Fill the buffer
	broker.Publish("event-1")

	// This should not block even though buffer is full
	done := make(chan struct{})
	go func() {
		broker.Publish("event-2")
		broker.Publish("event-3")
		close(done)
	}()

	select {
	case <-done:
		// Success - publish did not block
	case <-time.After(100 * time.Millisecond):
		t.Fatal("publish blocked on full channel")
	}
}

func TestBroker_ConcurrentOperations(t *testing.T) {
	broker := NewBroker[int]()
	defer broker.Shutdown()

	var wg sync.WaitGroup
	const numGoroutines = 100

	// Concurrent subscribes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			broker.Subscribe(ctx)
		}()
	}

	// Concurrent publishes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			broker.Publish(n)
		}(i)
	}

	wg.Wait()
}

func TestBroker_GenericTypes(t *testing.T) {
	type CustomPayload struct {
		ID   string
		Data map[string]any
	}

	broker := NewBroker[CustomPayload]()
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx)

	payload := CustomPayload{
		ID:   "test-123",
		Data: map[string]any{"key": "value"},
	}
	broker.Publish(payload)

	select {
	case event := <-events:
		assert.Equal(t, "test-123", event.Payload().ID)
		assert.Equal(t, "value", event.Payload().Data["key"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestWithBufferSize_InvalidValue(t *testing.T) {
	// Invalid values should be ignored
	broker := NewBroker[string](WithBufferSize(0))
	defer broker.Shutdown()

	assert.Equal(t, defaultBufferSize, broker.bufferSize)
}

func TestWithMaxEvents_InvalidValue(t *testing.T) {
	broker := NewBroker[string](WithMaxEvents(-1))
	defer broker.Shutdown()

	assert.Equal(t, defaultMaxEvents, broker.ring.Cap())
}

func TestBroker_Publish_StoresInRingBuffer(t *testing.T) {
	broker := NewBroker[string](WithMaxEvents(3))
	defer broker.Shutdown()

	broker.Publish("a")
	broker.Publish("b")
	broker.Publish("c")

	assert.Equal(t, 3, broker.ring.Len())

	// Publishing a 4th event should evict the oldest
	broker.Publish("d")
	assert.Equal(t, 3, broker.ring.Len())

	// Verify ring buffer contents (oldest to newest: b, c, d)
	entries := broker.ring.After(0)
	require.Len(t, entries, 3)
	assert.Equal(t, "b", entries[0].Value.Payload())
	assert.Equal(t, "c", entries[1].Value.Payload())
	assert.Equal(t, "d", entries[2].Value.Payload())
}

func TestBroker_Subscribe_After_ReplaysBuffered(t *testing.T) {
	broker := NewBroker[string](WithMaxEvents(10))
	defer broker.Shutdown()

	broker.Publish("first")  // seq 1
	broker.Publish("second") // seq 2
	broker.Publish("third")  // seq 3

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe after seq 1 — should replay "second" and "third"
	events := broker.Subscribe(ctx, After(1))

	var replayed []string
	for range 2 {
		select {
		case event := <-events:
			replayed = append(replayed, event.Payload())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for replayed event")
		}
	}

	assert.Equal(t, []string{"second", "third"}, replayed)
}

func TestBroker_Subscribe_After_ZeroReplaysAll(t *testing.T) {
	broker := NewBroker[string](WithMaxEvents(10))
	defer broker.Shutdown()

	broker.Publish("a")
	broker.Publish("b")
	broker.Publish("c")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx, After(0))

	var replayed []string
	for range 3 {
		select {
		case event := <-events:
			replayed = append(replayed, event.Payload())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for replayed event")
		}
	}

	assert.Equal(t, []string{"a", "b", "c"}, replayed)
}

func TestBroker_Subscribe_After_HighSeq_ReplaysNothing(t *testing.T) {
	broker := NewBroker[string](WithMaxEvents(10))
	defer broker.Shutdown()

	broker.Publish("a") // seq 1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx, After(100))

	// No replayed events expected
	select {
	case event := <-events:
		t.Fatalf("unexpected replayed event: %v", event.Payload())
	case <-time.After(50 * time.Millisecond):
		// Expected — nothing replayed
	}

	// But live events should still work
	broker.Publish("live")
	select {
	case event := <-events:
		assert.Equal(t, "live", event.Payload())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for live event")
	}
}

func TestBroker_Subscribe_After_Shutdown(t *testing.T) {
	broker := NewBroker[string]()
	broker.Shutdown()

	ctx := context.Background()
	events := broker.Subscribe(ctx, After(0))

	_, ok := <-events
	assert.False(t, ok, "channel should be immediately closed")
}

func TestBroker_RingBuffer_WrapAround(t *testing.T) {
	broker := NewBroker[int](WithMaxEvents(3))
	defer broker.Shutdown()

	// Fill buffer: [0, 1, 2]
	for i := range 3 {
		broker.Publish(i)
	}

	// Overwrite with more: [3, 4, 5] wrapping around
	for i := 3; i < 6; i++ {
		broker.Publish(i)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx, After(0))

	var values []int
	for range 3 {
		select {
		case event := <-events:
			values = append(values, event.Payload())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for replayed event")
		}
	}

	assert.Equal(t, []int{3, 4, 5}, values)
}

func TestBroker_Subscribe_After_ReplayedEventsHaveSeq(t *testing.T) {
	broker := NewBroker[string](WithMaxEvents(10))
	defer broker.Shutdown()

	broker.Publish("a") // seq 1
	broker.Publish("b") // seq 2
	broker.Publish("c") // seq 3

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx, After(0))

	var seqs []int64
	for range 3 {
		select {
		case event := <-events:
			seqs = append(seqs, event.Seq())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for replayed event")
		}
	}

	assert.Equal(t, []int64{1, 2, 3}, seqs)
}

func TestBroker_Publish_Blocking(t *testing.T) {
	// Buffer size 1 — without blocking, events would be dropped
	broker := NewBroker[string](
		WithBufferSize(1),
		WithBlockingPublish(),
	)
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx)

	// Publish 5 events — these will block until consumed
	const count = 5
	done := make(chan struct{})
	go func() {
		for i := range count {
			broker.Publish(fmt.Sprintf("event-%d", i))
		}
		close(done)
	}()

	var received []string
	for range count {
		event := <-events
		received = append(received, event.Payload())
	}

	<-done

	expected := []string{"event-0", "event-1", "event-2", "event-3", "event-4"}
	assert.Equal(t, expected, received)
}

func TestBroker_Publish_Blocking_ShutdownUnblocks(t *testing.T) {
	broker := NewBroker[string](
		WithBufferSize(1),
		WithBlockingPublish(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	broker.Subscribe(ctx)

	// Fill the buffer
	broker.Publish("fill")

	// This publish will block because buffer is full
	done := make(chan struct{})
	go func() {
		broker.Publish("blocked")
		close(done)
	}()

	// Give the goroutine time to block
	time.Sleep(20 * time.Millisecond)

	// Shutdown should unblock the publish
	broker.Shutdown()

	select {
	case <-done:
		// Success — shutdown unblocked the publish
	case <-time.After(100 * time.Millisecond):
		t.Fatal("shutdown did not unblock blocked publish")
	}
}

func TestBroker_Publish_Blocking_ContextCancelDoesNotPanic(t *testing.T) {
	// Regression test: canceling a subscriber's context while Publish is
	// blocked on that subscriber's channel must not panic with
	// "send on closed channel". The canceled subscriber should be skipped
	// and the publish should complete normally.
	broker := NewBroker[string](
		WithBufferSize(1),
		WithBlockingPublish(),
	)
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())

	broker.Subscribe(ctx)

	// Fill the subscriber's buffer so the next Publish will block.
	broker.Publish("fill")

	// Start a publish that will block waiting for the subscriber to drain.
	publishDone := make(chan struct{})
	go func() {
		broker.Publish("blocked")
		close(publishDone)
	}()

	// Give the goroutine a moment to enter the blocked select in Publish.
	time.Sleep(20 * time.Millisecond)

	// Cancel the subscriber's context. This closes the subscriber channel
	// while Publish is blocked trying to send to it — the race that caused
	// "send on closed channel" panics.
	cancel()

	select {
	case <-publishDone:
		// Success — Publish returned without panicking.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Publish did not return after subscriber context cancel")
	}
}

func TestBroker_Publish_Blocking_ContextCancelSkipsSubscriber(t *testing.T) {
	// When one subscriber's context is canceled mid-publish, the remaining
	// subscribers must still receive the event.
	broker := NewBroker[string](
		WithBufferSize(2),
		WithBlockingPublish(),
	)
	defer broker.Shutdown()

	cancelCtx, cancel := context.WithCancel(context.Background())
	stableCtx := t.Context()

	broker.Subscribe(cancelCtx)
	stable := broker.Subscribe(stableCtx)

	// Cancel the first subscriber before publishing.
	cancel()
	time.Sleep(20 * time.Millisecond) // let the unsubscribe goroutine run

	// Publish should reach the stable subscriber without panicking.
	broker.Publish("hello")

	select {
	case event := <-stable:
		assert.Equal(t, "hello", event.Payload())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stable subscriber did not receive event")
	}
}

func TestEvent_Timestamp(t *testing.T) {
	before := time.Now()
	broker := NewBroker[string](WithMaxEvents(10))
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := broker.Subscribe(ctx)
	broker.Publish("payload")

	select {
	case event := <-events:
		ts := event.Timestamp()
		assert.False(t, ts.Before(before), "timestamp should be after test start")
		assert.False(t, ts.After(time.Now()), "timestamp should be before now")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}
