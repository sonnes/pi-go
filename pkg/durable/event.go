package durable

import (
	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/sonnes/pi-go/pkg/stream"
)

// EventType categorizes durable events. Lifecycle events are delivered
// to the [Publisher] when their mutation commits; inner agent events
// flow on each run's [Stream].
type EventType string

const (
	// EventSessionInit is published when [New] binds a session, whether
	// newly created or resumed. LeafID is empty for a fresh session.
	EventSessionInit EventType = "session_init"

	// EventSessionBranched is published when [Agent.Branch] moves the
	// leaf pointer. FromID is the leaf before the move, and LeafID is the
	// leaf after it.
	EventSessionBranched EventType = "session_branched"

	// EventSessionForked is published on the source agent when
	// [Agent.Fork] copies the active path. SessionID identifies the child,
	// and ParentID identifies the source.
	EventSessionForked EventType = "session_forked"

	// EventSessionCompacted is published when [Agent.Compact] appends a
	// summary entry. Entries carries the [session.CompactionEntry].
	EventSessionCompacted EventType = "session_compacted"

	// EventAgent lifts an inner [agent.Event] verbatim onto the run
	// [Stream]. When the inner event marks a persistence boundary —
	// agent_start (run input) or message_end (each produced message) —
	// the event doubles as the durability receipt: it is forwarded
	// only after the entries are in the store, and carries them in
	// Entries along with the new LeafID.
	EventAgent EventType = "agent"
)

// Publisher receives lifecycle events after their mutation commits.
// The application owns delivery to channels, logs, or other observers.
//
// Publish runs synchronously outside the agent's locks, so an
// implementation may call back into the [Agent]. Keep it fast or hand
// work off because it runs on the mutating call's path.
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

	// LeafID is the leaf pointer after the event's effect.
	LeafID string

	// Entries are the persisted entries this event is the receipt for,
	// with store-assigned IDs.
	Entries []session.Entry
}

// Stream is the event stream of a single durable run, returned by
// [Agent.Run]. It is turn-scoped and carries only lifted inner agent
// events; lifecycle events go to the [Publisher]. The final result is
// the new messages the run produced.
//
// Events are forwarded only after everything they assert is persisted,
// so a consumed run is a persisted run. Errors surface on the Events
// iterator and from Wait, never as events.
type Stream = stream.Stream[Event, []ai.Message]

// Fail returns a [Stream] that produces no events and fails with err:
// Wait returns (nil, err) and the events iterator yields the error.
//
// It is how something standing in for a run refuses one — middleware
// wrapping [Agent.Run] that denies the run entirely returns Fail
// instead of calling through, and the caller sees an ordinary failed
// stream.
func Fail(err error) *Stream {
	return stream.Err[Event, []ai.Message](err)
}
