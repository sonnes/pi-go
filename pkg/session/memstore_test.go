package session_test

import (
	"context"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type codecState struct {
	Title string
	Model string
}

func TestMemoryStoreLoadNotFound(t *testing.T) {
	s := session.NewMemoryStore[codecState]()
	_, err := s.LoadSession(context.Background(), "nope")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
	_, err = s.LoadEntries(context.Background(), "nope")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestMemoryStoreCreateAndLoad(t *testing.T) {
	s := session.NewMemoryStore[codecState]()
	ctx := context.Background()

	require.NoError(t, s.CreateSession(ctx, &session.Session[codecState]{
		ID:    "s1",
		State: codecState{Title: "T"},
	}))

	sess, err := s.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "T", sess.State.Title)
	entries, err := s.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestMemoryStoreCreateExists(t *testing.T) {
	s := session.NewMemoryStore[codecState]()
	ctx := context.Background()
	require.NoError(t, s.CreateSession(ctx, &session.Session[codecState]{ID: "s1"}))
	err := s.CreateSession(ctx, &session.Session[codecState]{ID: "s1"})
	assert.ErrorIs(t, err, session.ErrSessionExists)
}

func TestMemoryStoreLoadIsolation(t *testing.T) {
	s := session.NewMemoryStore[codecState]()
	ctx := context.Background()
	require.NoError(t, s.CreateSession(ctx, &session.Session[codecState]{ID: "s1", State: codecState{Title: "T"}}))
	require.NoError(t, s.AppendEntries(ctx, "s1", session.NewMessageEntry(ai.UserMessage("a"))))

	sess, err := s.LoadSession(ctx, "s1")
	require.NoError(t, err)
	entries, err := s.LoadEntries(ctx, "s1")
	require.NoError(t, err)

	// Mutating the returned copies must not affect the store.
	sess.State.Title = "mutated"
	entries[0] = session.NewMessageEntry(ai.UserMessage("mutated"))

	sess2, err := s.LoadSession(ctx, "s1")
	require.NoError(t, err)
	entries2, err := s.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "T", sess2.State.Title)
	m, _ := session.AsMessageEntry(entries2[0])
	assert.Equal(t, "a", m.Text())
}

func TestMemoryStoreAppendUnknownSession(t *testing.T) {
	s := session.NewMemoryStore[codecState]()
	err := s.AppendEntries(context.Background(), "nope", session.NewMessageEntry(ai.UserMessage("x")))
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestMemoryStoreUpdateSession(t *testing.T) {
	s := session.NewMemoryStore[codecState]()
	ctx := context.Background()
	require.NoError(t, s.CreateSession(ctx, &session.Session[codecState]{ID: "s1", State: codecState{Title: "init"}}))

	require.NoError(t, s.UpdateSession(ctx, &session.Session[codecState]{
		ID:    "s1",
		State: codecState{Title: "renamed", Model: "opus"},
	}))

	sess, err := s.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "renamed", sess.State.Title)
	assert.Equal(t, "opus", sess.State.Model)
}

func TestMemoryStoreUpdateUnknownSession(t *testing.T) {
	s := session.NewMemoryStore[codecState]()
	err := s.UpdateSession(context.Background(), &session.Session[codecState]{ID: "nope"})
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestMemoryStoreAppendOrder(t *testing.T) {
	s := session.NewMemoryStore[codecState]()
	ctx := context.Background()
	require.NoError(t, s.CreateSession(ctx, &session.Session[codecState]{ID: "s1"}))

	require.NoError(t, s.AppendEntries(ctx, "s1", session.NewMessageEntry(ai.UserMessage("a"))))
	require.NoError(t, s.AppendEntries(ctx, "s1", session.NewMessageEntry(ai.UserMessage("b"))))

	entries, err := s.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	m0, _ := session.AsMessageEntry(entries[0])
	m1, _ := session.AsMessageEntry(entries[1])
	assert.Equal(t, "a", m0.Text())
	assert.Equal(t, "b", m1.Text())
}
