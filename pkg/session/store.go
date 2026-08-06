package session

import (
	"context"
	"errors"
)

// ErrSessionNotFound is returned by store operations for unknown
// session IDs.
var ErrSessionNotFound = errors.New("session: not found")

// ErrSessionExists is returned by [Store.CreateSession] when the session
// ID is already taken.
var ErrSessionExists = errors.New("session: already exists")

// Store persists sessions and their append-only entry logs. It is
// exactly the contract the durable agent consumes: session existence
// plus the transcript log. Application metadata does not pass through
// this interface — keep it in the application's own storage, keyed by
// the session ID.
type Store interface {
	// CreateSession registers a new session, forked from parentID when
	// it is non-empty. It returns [ErrSessionExists] if the session ID
	// is already taken.
	CreateSession(ctx context.Context, id, parentID string) error

	// LoadEntries returns a session's full entry log in append order, or
	// [ErrSessionNotFound] if the session does not exist.
	LoadEntries(ctx context.Context, sessionID string) ([]Entry, error)

	// AppendEntries appends entries to an existing session's log,
	// returning [ErrSessionNotFound] for unknown session IDs.
	AppendEntries(ctx context.Context, sessionID string, entries ...Entry) error
}
