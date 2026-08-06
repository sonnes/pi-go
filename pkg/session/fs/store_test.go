package fs_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/sonnes/pi-go/pkg/session/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ArtifactEntry struct {
	session.CustomEntry
	Title string
}

func init() {
	session.RegisterCustom("fs-artifact", ArtifactEntry{})
}

var fsTS = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestFileStoreCreateAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := fs.New(dir)
	require.NoError(t, err)
	ctx := context.Background()

	_, err = store.LoadSession(ctx, "s1")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
	_, err = store.LoadEntries(ctx, "s1")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)

	require.NoError(t, store.CreateSession(ctx, "s1", ""))

	sess, err := store.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", sess.ID)
	assert.Empty(t, sess.ParentID)
	assert.False(t, sess.CreatedAt.IsZero(), "store stamps CreatedAt")
	entries, err := store.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	assert.Empty(t, entries)

	// One file per session — no directory, no sidecar.
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "s1.jsonl", files[0].Name())
	assert.False(t, files[0].IsDir())
}

func TestFileStoreHeaderIsSessionInitEvent(t *testing.T) {
	dir := t.TempDir()
	store, _ := fs.New(dir)
	ctx := context.Background()

	require.NoError(t, store.CreateSession(ctx, "child", "parent"))

	data, err := os.ReadFile(filepath.Join(dir, "child.jsonl"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	require.Len(t, lines, 1)

	var header struct {
		Type      string    `json:"type"`
		ID        string    `json:"id"`
		ParentID  string    `json:"parent_id"`
		CreatedAt time.Time `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &header))
	assert.Equal(t, "session_init", header.Type)
	assert.Equal(t, "child", header.ID)
	assert.Equal(t, "parent", header.ParentID)
	assert.False(t, header.CreatedAt.IsZero())
}

func TestFileStoreCreateWithParent(t *testing.T) {
	store, _ := fs.New(t.TempDir())
	ctx := context.Background()

	require.NoError(t, store.CreateSession(ctx, "parent", ""))
	require.NoError(t, store.CreateSession(ctx, "child", "parent"))

	sess, err := store.LoadSession(ctx, "child")
	require.NoError(t, err)
	assert.Equal(t, "parent", sess.ParentID)
}

func TestFileStoreAppendUnknownSession(t *testing.T) {
	store, _ := fs.New(t.TempDir())
	err := store.AppendEntries(context.Background(), "nope", session.NewMessageEntry(ai.UserMessage("x")))
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestFileStoreCreateExists(t *testing.T) {
	store, _ := fs.New(t.TempDir())
	ctx := context.Background()
	require.NoError(t, store.CreateSession(ctx, "s1", ""))
	err := store.CreateSession(ctx, "s1", "")
	assert.ErrorIs(t, err, session.ErrSessionExists)
}

func TestFileStoreEntriesRoundTrip(t *testing.T) {
	store, _ := fs.New(t.TempDir())
	ctx := context.Background()
	require.NoError(t, store.CreateSession(ctx, "s1", ""))

	msg := session.MessageEntry{
		EntryHeader: session.EntryHeader{ID: "e1", CreatedAt: fsTS},
		Message:     ai.UserMessage("hi"),
	}
	art := ArtifactEntry{
		CustomEntry: session.CustomEntry{
			EntryHeader: session.EntryHeader{ID: "e2", ParentID: "e1", CreatedAt: fsTS},
			Kind:        "fs-artifact",
		},
		Title: "draft",
	}

	require.NoError(t, store.AppendEntries(ctx, "s1", msg))
	require.NoError(t, store.AppendEntries(ctx, "s1", art))

	entries, err := store.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	require.Len(t, entries, 2)

	m, ok := entries[0].(session.MessageEntry)
	require.True(t, ok)
	assert.Equal(t, "hi", m.Text())
	assert.Equal(t, "e1", m.ID)

	a, ok := entries[1].(ArtifactEntry)
	require.True(t, ok)
	assert.Equal(t, "draft", a.Title)
	assert.Equal(t, "e2", a.ID)
}

func TestFileStoreAppendDoesNotTouchHeader(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, _ := fs.New(dir)
	require.NoError(t, store.CreateSession(ctx, "s1", ""))
	created, err := store.LoadSession(ctx, "s1")
	require.NoError(t, err)

	require.NoError(t, store.AppendEntries(ctx, "s1", session.MessageEntry{
		EntryHeader: session.EntryHeader{ID: "e1", CreatedAt: fsTS},
		Message:     ai.UserMessage("hi"),
	}))

	reopened, _ := fs.New(dir)
	sess, err := reopened.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.True(t, created.CreatedAt.Equal(sess.CreatedAt), "appending entries leaves the header untouched")
}

func TestFileStoreReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	first, _ := fs.New(dir)
	require.NoError(t, first.CreateSession(ctx, "s1", ""))
	require.NoError(t, first.AppendEntries(ctx, "s1", session.MessageEntry{
		EntryHeader: session.EntryHeader{ID: "e1", CreatedAt: fsTS},
		Message:     ai.UserMessage("hi"),
	}))

	// A fresh store over the same directory (a new process) sees the data.
	second, err := fs.New(dir)
	require.NoError(t, err)
	sess, err := second.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", sess.ID)
	entries, err := second.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestFileStoreInvalidID(t *testing.T) {
	store, _ := fs.New(t.TempDir())
	ctx := context.Background()

	_, err := store.LoadSession(ctx, "a/b")
	assert.Error(t, err)
	_, err = store.LoadEntries(ctx, "a/b")
	assert.Error(t, err)
	assert.Error(t, store.CreateSession(ctx, "../escape", ""))
	assert.Error(t, store.AppendEntries(ctx, "", session.NewMessageEntry(ai.UserMessage("x"))))
}
