package session

import "time"

// Session is the durable identity and current state of an agent
// conversation. The state type T is application-defined — a title, an
// active model, a mode, or any struct the app tracks per session.
//
// State is the current (last-wins) snapshot. Every change to it is also
// logged as a [StateEntry] on the session's entry log, so the change
// history is auditable and the current value is recoverable from the log
// via [LatestState].
type Session[T any] struct {
	// ID identifies the session. The application chooses what it
	// represents — a user ID, ticket number, or random identifier.
	ID string `json:"id"`

	// ParentID is the ID of the session this one was forked from, or
	// empty.
	ParentID string `json:"parentId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// State is the current application-defined session state.
	State T `json:"state"`
}
