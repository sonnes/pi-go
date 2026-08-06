package session

import "time"

// Session records that a conversation exists and where it came from.
// It carries no application state: metadata like titles or modes
// belongs to the application's own storage, keyed by the session ID.
//
// Stores create records through [Store.CreateSession]; the concrete
// stores in this repository also read them back through a LoadSession
// method.
type Session struct {
	// ID identifies the session. The application chooses what it
	// represents — a user ID, ticket number, or random identifier.
	ID string `json:"id"`

	// ParentID is the ID of the session this one was forked from, or
	// empty.
	ParentID string `json:"parent_id,omitempty"`

	// CreatedAt is stamped by the store when the session is created.
	CreatedAt time.Time `json:"created_at"`
}
