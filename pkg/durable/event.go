package durable

import (
	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/sonnes/pi-go/pkg/stream"
)

// EventType categorizes durable events. Session lifecycle events are
// delivered to the [Publisher] the moment they happen; the lifted
// inner agent vocabulary flows on each run's [Stream].
type EventType string

const (
	// EventSessionInit is published when [New] binds the session —
	// created fresh or resumed from the store. LeafID is empty for a
	// fresh session.
	EventSessionInit EventType = "session_init"

	// EventSessionUpdated is published when [Agent.SetState] logs a
	// state change, carrying the appended [session.StateEntry].
	EventSessionUpdated EventType = "session_updated"

	// EventSessionBranched is published when [Agent.Branch] moves the
	// leaf pointer. FromID is the leaf before the move, LeafID after.
	EventSessionBranched EventType = "session_branched"

	// EventSessionForked is published on the source agent when
	// [Agent.Fork] copies the active path. SessionID is the new
	// session's ID, ParentID the source's.
	EventSessionForked EventType = "session_forked"

	// EventSessionCompacted is published when [Agent.Compact] appends a
	// summary entry, carrying the [session.CompactionEntry].
	EventSessionCompacted EventType = "session_compacted"

	// EventAgent lifts an inner [agent.Event] verbatim onto the run
	// [Stream]. When the inner event marks a persistence boundary —
	// agent_start (run input) or message_end (each produced message) —
	// the event doubles as the durability receipt: it is forwarded
	// only after the entries are in the store, and carries them in
	// Entries along with the new LeafID.
	EventAgent EventType = "agent"
)

// Publisher receives session events at the moment their mutation
// commits. The application owns delivery: forward to a channel, a
// websocket, a log — or fan out to many observers.
//
// Publish is called synchronously, after the mutation is durable and
// outside the agent's locks, so implementations may call back into the
// [Agent]. Calls from one goroutine arrive in mutation order; keep
// Publish fast or hand off, since it runs on the mutating call's path.
type Publisher interface {
	Publish(evt Event)
}

// PublisherFunc adapts a function to the [Publisher] interface.
type PublisherFunc func(evt Event)

// Publish implements [Publisher].
func (f PublisherFunc) Publish(evt Event) { f(evt) }

// Event is a single durable streaming event. Fields are populated
// based on Type — unused fields are zero-valued.
type Event struct {
	Type EventType

	// Agent is the lifted inner event (EventAgent).
	Agent *agent.Event

	// SessionID identifies the session (session_init) or the new
	// session (session_forked).
	SessionID string

	// ParentID is the source session (session_forked).
	ParentID string

	// FromID is the leaf before the move (session_branched).
	FromID string

	// LeafID is the leaf pointer after the event's effect.
	LeafID string

	// Entries are the persisted entries this event is the receipt for,
	// with store-assigned IDs.
	Entries []session.Entry
}

// Stream is the event stream of a single durable run, returned by
// [Agent.Run]. It is turn-scoped: lifted inner agent events only —
// session events go to the [Publisher]. The final result is the new
// messages the run produced.
//
// Events are forwarded only after everything they assert is persisted —
// a consumed run is a persisted run. Errors surface on the Events
// iterator and from Wait, never as events.
type Stream = stream.Stream[Event, []ai.Message]
