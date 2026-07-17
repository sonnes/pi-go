package session

import (
	"context"
	"errors"
)

// ErrSessionNotFound is returned by [Store.LoadSession] for unknown
// session IDs.
var ErrSessionNotFound = errors.New("session: not found")

// ErrSessionExists is returned by [Store.CreateSession] when the session
// ID is already taken.
var ErrSessionExists = errors.New("session: already exists")

// Store persists sessions and their entries. T is the session state type
// carried by [Session]. The application owns the schema and encoding.
//
// A session record is created by [Store.CreateSession] with its initial
// state. Session state (title, model, …) then changes by appending a
// [StateEntry]; [Store.LoadSession] returns the state as of the latest
// one (last-wins). All history is strictly append-only and returned in
// append order.
type Store[T any] interface {
	// CreateSession persists a new session, returning [ErrSessionExists]
	// if a session with the same ID already exists.
	CreateSession(ctx context.Context, s *Session[T]) error

	// LoadSession returns a session and its full entry log in append
	// order, or [ErrSessionNotFound] if the session does not exist.
	LoadSession(ctx context.Context, id string) (*Session[T], []Entry, error)

	// AppendEntries appends entries to an existing session's log,
	// returning [ErrSessionNotFound] for unknown session IDs. When the
	// batch includes a [StateEntry], the session's current
	// [Session.State], as returned by [Store.LoadSession], becomes the
	// latest one (last-wins).
	AppendEntries(ctx context.Context, sessionID string, entries ...Entry) error
}
