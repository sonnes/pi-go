// Package fs is a filesystem-backed [session.Store] implementation.
//
// Each session is a single JSONL file, <id>.jsonl: the first line is the
// [session.Session] record, written once by CreateSession; every
// following line is one log entry, append-only. Session state changes
// are [session.StateEntry] lines like any other entry, and the current
// state is folded from the log on load — nothing is ever rewritten, the
// file only grows.
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

// FileStore persists each session as one append-only JSONL file under a
// root directory. T is the session state type (see [session.Session]).
// Safe for concurrent use.
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

// CreateSession implements [session.Store]. It writes the session record
// as the first line of a new log file; O_EXCL makes the existence check
// atomic.
func (s *FileStore[T]) CreateSession(ctx context.Context, sess *session.Session[T]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.path(sess.ID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, os.ErrExist) {
		return session.ErrSessionExists
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// LoadSession implements [session.Store]. The returned session's State is
// the fold of the log: the latest [session.StateEntry] wins.
func (s *FileStore[T]) LoadSession(ctx context.Context, id string) (*session.Session[T], []session.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.path(id)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, session.ErrSessionNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)

	var sess session.Session[T]
	if err := dec.Decode(&sess); err != nil {
		return nil, nil, fmt.Errorf("fs: decode session %q: %w", id, err)
	}

	var entries []session.Entry
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("fs: decode entry in %q: %w", id, err)
		}
		e, err := session.UnmarshalEntry[T](raw)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, e)
	}

	if st, ok := session.LatestState[T](entries); ok {
		sess.State = st
	}
	return &sess, entries, nil
}

// AppendEntries implements [session.Store]. Entries are marshaled one per
// line and appended to the session's log; the session must already exist.
func (s *FileStore[T]) AppendEntries(ctx context.Context, sessionID string, entries ...session.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.path(sessionID)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	for _, e := range entries {
		line, err := session.MarshalEntry[T](e)
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

func (s *FileStore[T]) path(id string) (string, error) {
	if err := validateID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.root, id+".jsonl"), nil
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
