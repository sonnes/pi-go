// Package fs is a filesystem-backed [session.Store] implementation.
//
// Each session has its own directory. Mutable session metadata lives in
// session.json, while entries.jsonl is an append-only transcript log. The
// split lets metadata updates rewrite one snapshot without rewriting or
// interpreting transcript history.
//
// Custom entry types must be registered with [session.RegisterCustom]
// before they can be read back.
package fs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sonnes/pi-go/pkg/session"
)

const (
	sessionFilename = "session.json"
	entriesFilename = "entries.jsonl"
)

// FileStore persists mutable session records separately from append-only
// entry logs. T is the session state type (see [session.Session]). Safe
// for concurrent use.
type FileStore[T any] struct {
	mu   sync.Mutex
	root string
}

var _ session.Store[any] = (*FileStore[any])(nil)

// New creates a [FileStore] rooted at dir, creating the directory if it
// does not exist.
func New[T any](dir string) (*FileStore[T], error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &FileStore[T]{root: dir}, nil
}

// CreateSession implements [session.Store]. It creates the session's
// directory, metadata file, and empty entry log. Directory creation is
// the atomic existence check.
func (s *FileStore[T]) CreateSession(ctx context.Context, sess *session.Session[T]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir, err := s.sessionDir(sess.ID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}

	if err := os.Mkdir(dir, 0o755); errors.Is(err, os.ErrExist) {
		return session.ErrSessionExists
	} else if err != nil {
		return err
	}

	created := false
	defer func() {
		if !created {
			_ = os.RemoveAll(dir)
		}
	}()

	sessionPath := filepath.Join(dir, sessionFilename)
	if err := os.WriteFile(sessionPath, data, 0o644); err != nil {
		return err
	}
	entriesPath := filepath.Join(dir, entriesFilename)
	if err := os.WriteFile(entriesPath, nil, 0o644); err != nil {
		return err
	}

	created = true
	return nil
}

// UpdateSession implements [session.Store]. It replaces the mutable
// session record without touching the entry log.
func (s *FileStore[T]) UpdateSession(ctx context.Context, sess *session.Session[T]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.sessionPath(sess.ID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		return session.ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// LoadSession implements [session.Store].
func (s *FileStore[T]) LoadSession(ctx context.Context, id string) (*session.Session[T], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.sessionPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, session.ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	var sess session.Session[T]
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("fs: decode session %q: %w", id, err)
	}
	return &sess, nil
}

// LoadEntries implements [session.Store].
func (s *FileStore[T]) LoadEntries(ctx context.Context, sessionID string) ([]session.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.entriesPath(sessionID)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, session.ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var entries []session.Entry
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("fs: decode entry in %q: %w", sessionID, err)
		}
		e, err := session.UnmarshalEntry(raw)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// AppendEntries implements [session.Store]. Entries are marshaled one per
// line and appended without changing the session record.
func (s *FileStore[T]) AppendEntries(ctx context.Context, sessionID string, entries ...session.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.entriesPath(sessionID)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	for _, e := range entries {
		line, err := session.MarshalEntry(e)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		return session.ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (s *FileStore[T]) sessionDir(id string) (string, error) {
	if err := validateID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.root, id), nil
}

func (s *FileStore[T]) sessionPath(id string) (string, error) {
	dir, err := s.sessionDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionFilename), nil
}

func (s *FileStore[T]) entriesPath(id string) (string, error) {
	dir, err := s.sessionDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, entriesFilename), nil
}

// validateID rejects session IDs that are unsafe as filenames.
func validateID(id string) error {
	if id == "" {
		return errors.New("fs: empty session id")
	}
	if id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("fs: invalid session id %q", id)
	}
	return nil
}
