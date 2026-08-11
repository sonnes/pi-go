package session_test

import (
	"context"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStoreLoadNotFound(t *testing.T) {
	s := session.NewMemoryStore()
	_, err := s.LoadSession(context.Background(), "nope")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
	_, err = s.LoadEntries(context.Background(), "nope")
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestMemoryStoreCreateAndLoad(t *testing.T) {
	s := session.NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, s.CreateSession(ctx, "s1", ""))

	sess, err := s.LoadSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", sess.ID)
	assert.Empty(t, sess.ParentID)
	assert.False(t, sess.CreatedAt.IsZero(), "store stamps CreatedAt")
	entries, err := s.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestMemoryStoreCreateWithParent(t *testing.T) {
	s := session.NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, s.CreateSession(ctx, "parent", ""))
	require.NoError(t, s.CreateSession(ctx, "child", "parent"))

	sess, err := s.LoadSession(ctx, "child")
	require.NoError(t, err)
	assert.Equal(t, "parent", sess.ParentID)
}

func TestMemoryStoreCreateExists(t *testing.T) {
	s := session.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.CreateSession(ctx, "s1", ""))
	err := s.CreateSession(ctx, "s1", "")
	assert.ErrorIs(t, err, session.ErrSessionExists)
}

func TestMemoryStoreLoadIsolation(t *testing.T) {
	s := session.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.CreateSession(ctx, "s1", ""))
	require.NoError(t, s.AppendEntries(ctx, "s1", session.NewMessageEntry(ai.UserMessage("a"))))

	sess, err := s.LoadSession(ctx, "s1")
	require.NoError(t, err)
	entries, err := s.LoadEntries(ctx, "s1")
	require.NoError(t, err)

	// A change to the returned copies must not affect the store.
	sess.ID = "mutated"
	entries[0] = session.NewMessageEntry(ai.UserMessage("mutated"))

	sess2, err := s.LoadSession(ctx, "s1")
	require.NoError(t, err)
	entries2, err := s.LoadEntries(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", sess2.ID)
	m, _ := session.AsMessageEntry(entries2[0])
	assert.Equal(t, "a", m.Text())
}

func TestMemoryStoreAppendUnknownSession(t *testing.T) {
	s := session.NewMemoryStore()
	err := s.AppendEntries(context.Background(), "nope", session.NewMessageEntry(ai.UserMessage("x")))
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestMemoryStoreAppendOrder(t *testing.T) {
	s := session.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.CreateSession(ctx, "s1", ""))

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
