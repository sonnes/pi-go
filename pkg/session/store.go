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

// Store persists mutable sessions and their append-only entries. T is
// the application-defined state type carried by [Session]. Record and
// entry operations are separate so appending transcript history never
// changes application metadata as a hidden side effect.
type Store[T any] interface {
	// CreateSession persists a new session, returning [ErrSessionExists]
	// if a session with the same ID already exists.
	CreateSession(ctx context.Context, s *Session[T]) error

	// UpdateSession replaces an existing session record, returning
	// [ErrSessionNotFound] for an unknown session ID. It does not change
	// the session's entry log.
	UpdateSession(ctx context.Context, s *Session[T]) error

	// LoadSession returns a session record, or [ErrSessionNotFound] if
	// the session does not exist.
	LoadSession(ctx context.Context, id string) (*Session[T], error)

	// LoadEntries returns a session's full entry log in append order, or
	// [ErrSessionNotFound] if the session does not exist.
	LoadEntries(ctx context.Context, sessionID string) ([]Entry, error)

	// AppendEntries appends entries to an existing session's log,
	// returning [ErrSessionNotFound] for unknown session IDs. It does not
	// change the session record.
	AppendEntries(ctx context.Context, sessionID string, entries ...Entry) error
}
