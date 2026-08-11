package session

import "time"

// Session records that a conversation exists and where it came from.
// It carries no application state. Metadata such as titles or modes
// belongs to the application's own storage, keyed by the session ID.
//
// Stores create records through [Store.CreateSession]. The concrete
// stores in this repository also read them back through a LoadSession
// method.
type Session struct {
	// ID identifies the session. The application chooses what it
	// represents: a user ID, a ticket number, or a random identifier.
	ID string `json:"id"`

	// ParentID is the ID of the session that this one was forked from.
	// It is empty when the session has no parent.
	ParentID string `json:"parent_id,omitempty"`

	// CreatedAt is the creation time. The store sets this field.
	CreatedAt time.Time `json:"created_at"`
}
