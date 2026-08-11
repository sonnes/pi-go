package ai

import (
	"context"

	"github.com/sonnes/pi-go/pkg/stream"
)

// EventStream is the stream of [Event] values from one model call. It ends
// with the final [Message]. It is a [stream.Stream]. Use Events() to iterate
// individual events. Use Wait() to block for the final message. If the
// producer returns an error, that error surfaces on the Events iterator and
// from Wait.
type EventStream = stream.Stream[Event, *Message]

// NewEventStream creates a stream. The producer runs in a goroutine. It
// pushes events through the callback and returns the final message.
// NewEventStream is the entry point for [TextProvider] implementations.
func NewEventStream(fn func(push func(Event)) (*Message, error)) *EventStream {
	return stream.New(fn)
}

// GenerateText streams a text response from lm and blocks for the final
// message. It is a convenience wrapper around lm.StreamText(...).Wait().
func GenerateText(ctx context.Context, lm LanguageModel, p Prompt, opts ...Option) (*Message, error) {
	return lm.StreamText(ctx, p, opts...).Wait()
}

// ErrStream returns an [EventStream] that immediately fails with err. Use
// it to surface a pre-flight error on the stream instead of a separate
// error return.
func ErrStream(err error) *EventStream {
	return stream.Err[Event, *Message](err)
}
