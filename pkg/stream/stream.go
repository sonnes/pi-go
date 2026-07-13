// Package stream provides a generic single-run event stream connecting
// one producer goroutine to one consumer, with dual consumption
// patterns: iterate individual events via [Stream.Events], or block for
// the final result via [Stream.Wait].
package stream

import (
	"iter"
	"sync"
)

// Stream carries events of type T from a producer to a single
// consumer, ending with a final result of type R.
//
// A Stream is single-consumer: consume it from one goroutine. Breaking
// out of Events early does not stop the producer — it keeps running to
// completion with subsequent pushes dropped; use Wait to block until it
// finishes.
type Stream[T, R any] struct {
	ch       chan T
	stop     chan struct{}
	stopOnce sync.Once
	result   R
	err      error
}

// New runs fn in a goroutine, delivering pushed events to the stream's
// consumer. The values fn returns become the stream's final result,
// returned by [Stream.Wait].
func New[T, R any](fn func(push func(T)) (R, error)) *Stream[T, R] {
	s := &Stream[T, R]{
		ch:   make(chan T, 16),
		stop: make(chan struct{}),
	}
	go func() {
		defer close(s.ch)
		s.result, s.err = fn(func(e T) {
			select {
			case s.ch <- e:
			case <-s.stop:
			}
		})
	}()
	return s
}

// Events returns an iterator over the stream's events. If the producer
// fails, the final iteration yields a zero T with the producer's error;
// otherwise the yielded error is always nil.
func (s *Stream[T, R]) Events() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for e := range s.ch {
			if !yield(e, nil) {
				s.abandon()
				return
			}
		}
		if s.err != nil {
			var zero T
			yield(zero, s.err)
		}
	}
}

// Wait blocks until the producer completes, discarding any remaining
// events, and returns the final result. On failure it returns whatever
// partial result the producer returned along with its error.
func (s *Stream[T, R]) Wait() (R, error) {
	s.abandon()
	for range s.ch {
	}
	return s.result, s.err
}

// abandon stops event delivery: subsequent pushes from the producer
// are dropped instead of blocking on an unread channel.
func (s *Stream[T, R]) abandon() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// Err returns a [Stream] that immediately fails with err.
func Err[T, R any](err error) *Stream[T, R] {
	return New(func(func(T)) (R, error) {
		var zero R
		return zero, err
	})
}
