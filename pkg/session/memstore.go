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

// UpdateSession implements [Store].
func (s *MemoryStore[T]) UpdateSession(ctx context.Context, sess *Session[T]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[sess.ID]; !ok {
		return ErrSessionNotFound
	}
	cp := *sess
	s.sessions[sess.ID] = &cp
	return nil
}

// LoadSession implements [Store].
func (s *MemoryStore[T]) LoadSession(ctx context.Context, id string) (*Session[T], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	cp := *sess
	return &cp, nil
}

// LoadEntries implements [Store].
func (s *MemoryStore[T]) LoadEntries(ctx context.Context, sessionID string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[sessionID]; !ok {
		return nil, ErrSessionNotFound
	}

	src := s.entries[sessionID]
	entries := make([]Entry, len(src))
	copy(entries, src)
	return entries, nil
}

// AppendEntries implements [Store].
func (s *MemoryStore[T]) AppendEntries(ctx context.Context, sessionID string, entries ...Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[sessionID]; !ok {
		return ErrSessionNotFound
	}

	s.entries[sessionID] = append(s.entries[sessionID], entries...)
	return nil
}
