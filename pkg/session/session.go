package session

import "time"

// Session is the durable identity and current state of an agent
// conversation. The state type T is application-defined — a title, an
// active model, a mode, or any struct the app tracks per session.
//
// Session records are mutable independently of the session's append-only
// entry log. Applications replace a record through [Store.UpdateSession],
// while durable agents operate on the log without caching this record.
type Session[T any] struct {
	// ID identifies the session. The application chooses what it
	// represents — a user ID, ticket number, or random identifier.
	ID string `json:"id"`

	// ParentID is the ID of the session this one was forked from, or
	// empty.
	ParentID string `json:"parent_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// State is the current application-defined session state.
	State T `json:"state"`
}
