package session

import (
	"context"
	"sync"
)

// MemoryStore is an in-memory [Store] for tests and examples.
// Safe for concurrent use; returned sessions and slices are copies, so
// callers never share mutable state with the store.
type MemoryStore[T any] struct {
	mu       sync.Mutex
	sessions map[string]*Session[T]
	entries  map[string][]Entry
}

var _ Store[any] = (*MemoryStore[any])(nil)

// NewMemoryStore creates an empty [MemoryStore].
func NewMemoryStore[T any]() *MemoryStore[T] {
	return &MemoryStore[T]{
		sessions: make(map[string]*Session[T]),
		entries:  make(map[string][]Entry),
	}
}

// CreateSession implements [Store].
func (s *MemoryStore[T]) CreateSession(ctx context.Context, sess *Session[T]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[sess.ID]; ok {
		return ErrSessionExists
	}
	cp := *sess
	s.sessions[sess.ID] = &cp
	return nil
}

// LoadSession implements [Store].
func (s *MemoryStore[T]) LoadSession(ctx context.Context, id string) (*Session[T], []Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, nil, ErrSessionNotFound
	}
	cp := *sess

	src := s.entries[id]
	entries := make([]Entry, len(src))
	copy(entries, src)

	return &cp, entries, nil
}

// AppendEntries implements [Store]. When the batch includes a
// [StateEntry], the session's current State is updated to the latest one
// (last-wins).
func (s *MemoryStore[T]) AppendEntries(ctx context.Context, sessionID string, entries ...Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	s.entries[sessionID] = append(s.entries[sessionID], entries...)

	if st, ok := LatestState[T](entries); ok {
		sess.State = st
	}
	return nil
}
