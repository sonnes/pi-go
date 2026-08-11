// Package stream provides a generic single-run event stream. The stream
// connects one producer goroutine to one consumer. It supports two
// consumption patterns: iterate individual events with [Stream.Events],
// or block for the final result with [Stream.Wait].
package stream

import (
	"iter"
	"sync"
)

// Stream carries events of type T from a producer to a single consumer.
// It ends with a final result of type R.
//
// A Stream has one consumer. Consume it from one goroutine. An early
// break out of Events does not stop the producer. The producer runs to
// completion, and the stream discards the later pushes. Use Wait to
// block until the producer finishes.
type Stream[T, R any] struct {
	ch       chan T
	stop     chan struct{}
	stopOnce sync.Once
	result   R
	err      error
}

// New runs fn in a goroutine and delivers pushed events to the consumer
// of the stream. The values that fn returns become the final result of
// the stream, which [Stream.Wait] returns.
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

// Events returns an iterator over the events of the stream. If the
// producer fails, the final iteration yields a zero T with the error of
// the producer. In every other case, the yielded error is nil.
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

// Wait blocks until the producer completes and discards any remaining
// events. It returns the final result. If the producer fails, Wait
// returns the partial result of the producer with its error.
func (s *Stream[T, R]) Wait() (R, error) {
	s.abandon()
	for range s.ch {
	}
	return s.result, s.err
}

// abandon stops event delivery. The stream discards later pushes from
// the producer, and the producer does not block on an unread channel.
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
