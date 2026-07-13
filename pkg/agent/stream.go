package agent

import (
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/stream"
)

// Stream is the event stream of a single agent run, returned by
// [Agent.Run]: a [stream.Stream] of [Event] values whose final result
// is the new messages the run produced.
//
// Consume it event by event via Events, or block for the result via
// Wait. Breaking out of Events early does not stop the run — cancel the
// context passed to [Agent.Run] to abort it.
type Stream = stream.Stream[Event, []ai.Message]

// NewStream runs fn in a goroutine, delivering pushed events to the
// stream's consumer. The values fn returns become the stream's final
// result, returned by [Stream.Wait]. Backends implement [Agent.Run] by
// wrapping their run logic with NewStream.
func NewStream(fn func(push func(Event)) ([]ai.Message, error)) *Stream {
	return stream.New(fn)
}

// errStream returns a [Stream] that immediately fails with err.
func errStream(err error) *Stream {
	return stream.Err[Event, []ai.Message](err)
}
