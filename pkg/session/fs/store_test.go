package fs_test

import (
	"context"
	"os"
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

	_, _, err = store.LoadSession(ctx, "s1")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)

	require.NoError(t, store.CreateSession(ctx, &session.Session[fsState]{
		ID:        "s1",
		CreatedAt: fsTS,
		UpdatedAt: fsTS,
		State:     fsState{Title: "T", Model: "M"},
	}))

	sess, entries, err := store.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", sess.ID)
	assert.Equal(t, fsState{Title: "T", Model: "M"}, sess.State)
	assert.True(t, fsTS.Equal(sess.CreatedAt))
	assert.Empty(t, entries)

	// One file per session: the record is line 1 of the log itself.
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "s1.jsonl", files[0].Name())
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

func TestFileStoreEntriesRoundTrip(t *testing.T) {
	store, _ := fs.New[fsState](t.TempDir())
	ctx := context.Background()
	require.NoError(t, store.CreateSession(ctx, &session.Session[fsState]{ID: "s1"}))

	msg := session.MessageEntry{
		EntryHeader: session.EntryHeader{ID: "e1", CreatedAt: fsTS},
		Message:     ai.UserMessage("hi"),
	}
	st := session.StateEntry[fsState]{
		EntryHeader: session.EntryHeader{ID: "e2", ParentID: "e1", CreatedAt: fsTS},
		State:       fsState{Title: "T"},
	}
	art := ArtifactEntry{
		CustomEntry: session.CustomEntry{
			EntryHeader: session.EntryHeader{ID: "e3", ParentID: "e2", CreatedAt: fsTS},
			Kind:        "fs-artifact",
		},
		Title: "draft",
	}

	require.NoError(t, store.AppendEntries(ctx, "s1", msg, st))
	require.NoError(t, store.AppendEntries(ctx, "s1", art))

	_, entries, err := store.LoadSession(ctx, "s1")
	require.NoError(t, err)
	require.Len(t, entries, 3)

	m, ok := entries[0].(session.MessageEntry)
	require.True(t, ok)
	assert.Equal(t, "hi", m.Text())
	assert.Equal(t, "e1", m.ID)

	s2, ok := entries[1].(session.StateEntry[fsState])
	require.True(t, ok)
	assert.Equal(t, "T", s2.State.Title)
	assert.Equal(t, "e1", s2.ParentID)

	a, ok := entries[2].(ArtifactEntry)
	require.True(t, ok)
	assert.Equal(t, "draft", a.Title)
	assert.Equal(t, "e3", a.ID)
}

func TestFileStoreStateEventUpdatesSession(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, _ := fs.New[fsState](dir)
	require.NoError(t, store.CreateSession(ctx, &session.Session[fsState]{ID: "s1", State: fsState{Title: "init"}}))
	require.NoError(t, store.AppendEntries(ctx, "s1", session.NewStateEntry(fsState{Title: "renamed", Model: "opus"})))

	sess, _, err := store.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "renamed", sess.State.Title)
	assert.Equal(t, "opus", sess.State.Model)

	// The updated state is persisted to the record, not just folded in memory.
	reopened, _ := fs.New[fsState](dir)
	sess2, _, err := reopened.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "renamed", sess2.State.Title)
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
	sess, entries, err := second.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "T", sess.State.Title)
	require.Len(t, entries, 1)
}

func TestFileStoreInvalidID(t *testing.T) {
	store, _ := fs.New[fsState](t.TempDir())
	ctx := context.Background()

	_, _, err := store.LoadSession(ctx, "a/b")
	assert.Error(t, err)
	assert.Error(t, store.CreateSession(ctx, &session.Session[fsState]{ID: "../escape"}))
	assert.Error(t, store.AppendEntries(ctx, "", session.NewMessageEntry(ai.UserMessage("x"))))
}
