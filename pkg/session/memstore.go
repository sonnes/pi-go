package session

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is an in-memory [Store] for tests and examples. It is safe
// for concurrent use. The returned sessions and slices are copies, so a
// caller never shares mutable state with the store.
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
	entries  map[string][]Entry
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore creates an empty [MemoryStore].
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]*Session),
		entries:  make(map[string][]Entry),
	}
}

// CreateSession implements [Store].
func (s *MemoryStore) CreateSession(ctx context.Context, id, parentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[id]; ok {
		return ErrSessionExists
	}
	s.sessions[id] = &Session{
		ID:        id,
		ParentID:  parentID,
		CreatedAt: time.Now(),
	}
	return nil
}

// LoadSession returns a session's record. If the session does not
// exist, it returns [ErrSessionNotFound].
func (s *MemoryStore) LoadSession(ctx context.Context, id string) (*Session, error) {
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
func (s *MemoryStore) LoadEntries(ctx context.Context, sessionID string) ([]Entry, error) {
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
func (s *MemoryStore) AppendEntries(ctx context.Context, sessionID string, entries ...Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[sessionID]; !ok {
		return ErrSessionNotFound
	}

	s.entries[sessionID] = append(s.entries[sessionID], entries...)
	return nil
}
