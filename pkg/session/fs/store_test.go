package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/sonnes/pi-go/pkg/session/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fsState struct {
	Title string
	Model string
}

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
	store, err := fs.New[fsState](dir)
	require.NoError(t, err)
	ctx := context.Background()

	_, err = store.LoadSession(ctx, "s1")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
	_, err = store.LoadEntries(ctx, "s1")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)

	require.NoError(t, store.CreateSession(ctx, &session.Session[fsState]{
		ID:        "s1",
		CreatedAt: fsTS,
		UpdatedAt: fsTS,
		State:     fsState{Title: "T", Model: "M"},
	}))

	sess, err := store.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", sess.ID)
	assert.Equal(t, fsState{Title: "T", Model: "M"}, sess.State)
	assert.True(t, fsTS.Equal(sess.CreatedAt))
	entries, err := store.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	assert.Empty(t, entries)

	// Session metadata and append-only entries have separate files.
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "s1", files[0].Name())
	assert.True(t, files[0].IsDir())

	files, err = os.ReadDir(filepath.Join(dir, "s1"))
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "entries.jsonl", files[0].Name())
	assert.Equal(t, "session.json", files[1].Name())
}

func TestFileStoreAppendUnknownSession(t *testing.T) {
	store, _ := fs.New[fsState](t.TempDir())
	err := store.AppendEntries(context.Background(), "nope", session.NewMessageEntry(ai.UserMessage("x")))
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestFileStoreCreateExists(t *testing.T) {
	store, _ := fs.New[fsState](t.TempDir())
	ctx := context.Background()
	require.NoError(t, store.CreateSession(ctx, &session.Session[fsState]{ID: "s1"}))
	err := store.CreateSession(ctx, &session.Session[fsState]{ID: "s1"})
	assert.ErrorIs(t, err, session.ErrSessionExists)
}

func TestFileStoreUpdateUnknownSession(t *testing.T) {
	store, _ := fs.New[fsState](t.TempDir())
	err := store.UpdateSession(context.Background(), &session.Session[fsState]{ID: "nope"})
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestFileStoreEntriesRoundTrip(t *testing.T) {
	store, _ := fs.New[fsState](t.TempDir())
	ctx := context.Background()
	require.NoError(t, store.CreateSession(ctx, &session.Session[fsState]{ID: "s1"}))

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

func TestFileStoreUpdateSession(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, _ := fs.New[fsState](dir)
	require.NoError(t, store.CreateSession(ctx, &session.Session[fsState]{ID: "s1", State: fsState{Title: "init"}}))
	require.NoError(t, store.UpdateSession(ctx, &session.Session[fsState]{
		ID:    "s1",
		State: fsState{Title: "renamed", Model: "opus"},
	}))

	sess, err := store.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "renamed", sess.State.Title)
	assert.Equal(t, "opus", sess.State.Model)

	reopened, _ := fs.New[fsState](dir)
	sess2, err := reopened.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "renamed", sess2.State.Title)
}

func TestFileStoreAppendDoesNotUpdateSession(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, _ := fs.New[fsState](dir)
	require.NoError(t, store.CreateSession(ctx, &session.Session[fsState]{
		ID:        "s1",
		CreatedAt: fsTS,
		UpdatedAt: fsTS,
	}))

	appended := fsTS.Add(time.Hour)
	require.NoError(t, store.AppendEntries(ctx, "s1", session.MessageEntry{
		EntryHeader: session.EntryHeader{ID: "e1", CreatedAt: appended},
		Message:     ai.UserMessage("hi"),
	}))

	reopened, _ := fs.New[fsState](dir)
	sess, err := reopened.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.True(t, fsTS.Equal(sess.UpdatedAt), "appending entries leaves session metadata untouched")
	assert.True(t, fsTS.Equal(sess.CreatedAt), "createdAt is untouched")
}

func TestFileStoreReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	first, _ := fs.New[fsState](dir)
	require.NoError(t, first.CreateSession(ctx, &session.Session[fsState]{ID: "s1", CreatedAt: fsTS, State: fsState{Title: "T"}}))
	require.NoError(t, first.AppendEntries(ctx, "s1", session.MessageEntry{
		EntryHeader: session.EntryHeader{ID: "e1", CreatedAt: fsTS},
		Message:     ai.UserMessage("hi"),
	}))

	// A fresh store over the same directory (a new process) sees the data.
	second, err := fs.New[fsState](dir)
	require.NoError(t, err)
	sess, err := second.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "T", sess.State.Title)
	entries, err := second.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestFileStoreInvalidID(t *testing.T) {
	store, _ := fs.New[fsState](t.TempDir())
	ctx := context.Background()

	_, err := store.LoadSession(ctx, "a/b")
	assert.Error(t, err)
	_, err = store.LoadEntries(ctx, "a/b")
	assert.Error(t, err)
	assert.Error(t, store.CreateSession(ctx, &session.Session[fsState]{ID: "../escape"}))
	assert.Error(t, store.UpdateSession(ctx, &session.Session[fsState]{ID: "../escape"}))
	assert.Error(t, store.AppendEntries(ctx, "", session.NewMessageEntry(ai.UserMessage("x"))))
}
