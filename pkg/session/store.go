package session

import (
	"context"
	"errors"
)

// ErrSessionNotFound marks an unknown session ID. Store operations
// return it.
var ErrSessionNotFound = errors.New("session: not found")

// ErrSessionExists marks a session ID that is already taken.
// [Store.CreateSession] returns it.
var ErrSessionExists = errors.New("session: already exists")

// Store persists sessions and their append-only entry logs. It is
// exactly the contract that the durable agent consumes: session
// existence plus the transcript log. Application metadata does not pass
// through this interface. It belongs in the application's own storage,
// keyed by the session ID.
type Store interface {
	// CreateSession registers a new session. If parentID is not empty,
	// the new session is forked from it. If the session ID is already
	// taken, CreateSession returns [ErrSessionExists].
	CreateSession(ctx context.Context, id, parentID string) error

	// LoadEntries returns a session's full entry log in append order. If
	// the session does not exist, it returns [ErrSessionNotFound].
	LoadEntries(ctx context.Context, sessionID string) ([]Entry, error)

	// AppendEntries appends entries to an existing session's log. For an
	// unknown session ID, it returns [ErrSessionNotFound].
	AppendEntries(ctx context.Context, sessionID string, entries ...Entry) error
}
