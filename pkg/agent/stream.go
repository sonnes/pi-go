package agent

import (
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/stream"
)

// Stream is the event stream of a single agent run, which [Agent.Run]
// returns. Stream is a [stream.Stream] of [Event] values. Its final
// result is the new messages that the run produced.
//
// Read the stream event by event with Events, or block for the result
// with Wait. An early break out of Events does not stop the run. To stop
// the run, cancel the context that you passed to [Agent.Run].
type Stream = stream.Stream[Event, []ai.Message]

// NewStream runs fn in a goroutine and delivers the pushed events to the
// consumer of the stream. The values that fn returns become the final
// result of the stream, which [Stream.Wait] returns. A backend wraps its
// run logic with NewStream to implement [Agent.Run].
func NewStream(fn func(push func(Event)) ([]ai.Message, error)) *Stream {
	return stream.New(fn)
}

// ErrStream returns a [Stream] that fails immediately with err. A backend
// uses it to report pre-flight errors on the stream. ErrStream mirrors
// [ai.ErrStream].
func ErrStream(err error) *Stream {
	return stream.Err[Event, []ai.Message](err)
}
