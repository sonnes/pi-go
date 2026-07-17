package ai

import (
	"context"
	"fmt"

	"github.com/sonnes/pi-go/pkg/stream"
)

// EventStream is the stream of [Event]s produced by one model call,
// ending with the final [Message]. It is a [stream.Stream]: iterate
// individual events via Events(), or block for the final message via
// Wait(). The producer's error, if any, surfaces on the Events iterator
// and from Wait.
type EventStream = stream.Stream[Event, *Message]

// NewEventStream creates a stream. The producer runs in a goroutine,
// pushes events via the callback, and returns the final message. It is
// the entry point for [Provider] implementations.
func NewEventStream(fn func(push func(Event)) (*Message, error)) *EventStream {
	return stream.New(fn)
}

// StreamText streams a text response from the model.
func StreamText(ctx context.Context, model Model, p Prompt, opts ...Option) *EventStream {
	prov, ok := GetProvider(model.Provider)
	if !ok {
		return errStream(fmt.Errorf("ai: no provider registered for %q", model.Provider))
	}
	o := ApplyOptions(opts)
	return prov.StreamText(ctx, model, p, o)
}

// GenerateText generates a text response synchronously.
// Convenience wrapper around StreamText(...).Wait().
func GenerateText(ctx context.Context, model Model, p Prompt, opts ...Option) (*Message, error) {
	return StreamText(ctx, model, p, opts...).Wait()
}

// errStream returns an [EventStream] that immediately fails with err.
func errStream(err error) *EventStream {
	return stream.Err[Event, *Message](err)
}
