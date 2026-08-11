// Package fs is a filesystem-backed [session.Store] implementation.
//
// Each session is a single JSONL file, <id>.jsonl. The first line is a
// session_init event that carries the [session.Session] record.
// CreateSession writes this line once. Every following line is one log
// entry, and the log is append-only. The store never rewrites a line.
// The file only grows.
//
// Custom entry types must be registered with [session.RegisterCustom]
// before the store can read them back.
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
	"time"

	"github.com/sonnes/pi-go/pkg/session"
)

// headerType tags the first line of a session file, so every line in
// the log carries a type discriminator.
const headerType = "session_init"

// header is the wire shape of a session file's first line: the session
// record inlined under a session_init tag.
type header struct {
	Type string `json:"type"`
	session.Session
}

// FileStore persists each session as one append-only JSONL file under
// a root directory. It is safe for concurrent use.
type FileStore struct {
	mu   sync.Mutex
	root string
}

var _ session.Store = (*FileStore)(nil)

// New creates a [FileStore] rooted at dir. If the directory does not
// exist, New creates it.
func New(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &FileStore{root: dir}, nil
}

// CreateSession implements [session.Store]. It writes the session
// record as the session_init line of a new log file. O_EXCL makes the
// existence check atomic.
func (s *FileStore) CreateSession(ctx context.Context, id, parentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.path(id)
	if err != nil {
		return err
	}
	h := header{
		Type: headerType,
		Session: session.Session{
			ID:        id,
			ParentID:  parentID,
			CreatedAt: time.Now(),
		},
	}
	data, err := json.Marshal(h)
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

// LoadSession returns the session record from a log file's first line.
// If the session does not exist, it returns
// [session.ErrSessionNotFound].
func (s *FileStore) LoadSession(ctx context.Context, id string) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.open(id)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sess, _, err := decodeHeader(f, id)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// LoadEntries implements [session.Store]. It skips the session_init
// line and decodes the rest of the log.
func (s *FileStore) LoadEntries(ctx context.Context, sessionID string) ([]session.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.open(sessionID)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	_, dec, err := decodeHeader(f, sessionID)
	if err != nil {
		return nil, err
	}

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

// AppendEntries implements [session.Store]. It marshals one entry per
// line and appends the lines. It never touches the session_init line.
func (s *FileStore) AppendEntries(ctx context.Context, sessionID string, entries ...session.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.path(sessionID)
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

// open opens a session's log file. For a missing file, it returns
// [session.ErrSessionNotFound].
func (s *FileStore) open(id string) (*os.File, error) {
	path, err := s.path(id)
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
	return f, nil
}

// decodeHeader reads the session_init line from the front of a log file.
// It returns a decoder positioned at the first entry.
func decodeHeader(f *os.File, id string) (*session.Session, *json.Decoder, error) {
	dec := json.NewDecoder(f)
	var h header
	if err := dec.Decode(&h); err != nil {
		return nil, nil, fmt.Errorf("fs: decode session %q: %w", id, err)
	}
	return &h.Session, dec, nil
}

func (s *FileStore) path(id string) (string, error) {
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
