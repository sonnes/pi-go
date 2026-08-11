package durable

import (
	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/sonnes/pi-go/pkg/stream"
)

// EventType categorizes durable events. Lifecycle events reach the
// [Publisher] when their mutation commits. Inner agent events flow on
// the [Stream] of each run.
type EventType string

const (
	// EventSessionInit is the event that [New] publishes when it binds a
	// session, either new or resumed. For a fresh session, LeafID is
	// empty.
	EventSessionInit EventType = "session_init"

	// EventSessionBranched is the event that [Agent.Branch] publishes
	// when it moves the leaf pointer. FromID is the leaf before the move.
	// LeafID is the leaf after the move.
	EventSessionBranched EventType = "session_branched"

	// EventSessionForked is the event that [Agent.Fork] publishes on the
	// source agent when it copies the active path. SessionID identifies
	// the child. ParentID identifies the source.
	EventSessionForked EventType = "session_forked"

	// EventSessionCompacted is the event that [Agent.Compact] publishes
	// when it appends a summary entry. Entries carries the
	// [session.CompactionEntry].
	EventSessionCompacted EventType = "session_compacted"

	// EventAgent lifts an inner [agent.Event] verbatim onto the run
	// [Stream]. Two inner events mark a persistence boundary: agent_start
	// for the run input, and message_end for each produced message. At
	// such a boundary, the event also works as the durability receipt.
	// The agent forwards it only after the entries are in the store, and
	// the event carries them in Entries with the new LeafID.
	EventAgent EventType = "agent"
)

// Publisher receives lifecycle events after their mutation commits.
// The application owns delivery to channels, logs, or other observers.
//
// Publish runs synchronously outside the locks of the agent, so an
// implementation can call back into the [Agent]. Publish runs on the
// path of the mutating call. Keep it fast, or move the work to another
// goroutine.
type Publisher interface {
	Publish(evt Event)
}

// PublisherFunc adapts a function to the [Publisher] interface.
type PublisherFunc func(evt Event)

// Publish implements [Publisher].
func (f PublisherFunc) Publish(evt Event) { f(evt) }

// Event reports one durable lifecycle or run event. Fields that do not
// apply to Type are zero-valued.
type Event struct {
	Type EventType

	// Agent is the lifted inner event (EventAgent).
	Agent *agent.Event

	// SessionID identifies the initialized session or forked child.
	SessionID string

	// ParentID identifies the source session for EventSessionForked.
	ParentID string

	// FromID is the leaf before EventSessionBranched moves it.
	FromID string

	// LeafID is the leaf pointer after the effect of the event.
	LeafID string

	// Entries are the persisted entries that this event is the receipt
	// for, with store-assigned IDs.
	Entries []session.Entry
}

// Stream is the event stream of a single durable run. [Agent.Run]
// returns it. The stream is turn-scoped and carries only lifted inner
// agent events. Lifecycle events go to the [Publisher]. The final
// result is the new messages that the run produced.
//
// The agent forwards an event only after everything the event asserts
// is persisted. A consumed run is therefore a persisted run. Errors
// surface on the Events iterator and from Wait, never as events.
type Stream = stream.Stream[Event, []ai.Message]

// Fail returns a [Stream] that produces no events and fails with err.
// Wait returns (nil, err), and the events iterator yields the error.
//
// A stand-in for a run uses Fail to refuse the run. Middleware around
// [Agent.Run] that denies the run returns Fail instead of a call to the
// next runner. The caller then sees an ordinary failed stream.
func Fail(err error) *Stream {
	return stream.Err[Event, []ai.Message](err)
}
