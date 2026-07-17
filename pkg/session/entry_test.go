package session_test

import (
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMessageEntry(t *testing.T) {
	m := ai.UserMessage("hi")
	e := session.NewMessageEntry(m)
	assert.Equal(t, "hi", e.Text())
	assert.Empty(t, e.ID, "tree fields assigned on append, not construction")
}

func TestNewStateEntry(t *testing.T) {
	type st struct{ Title string }
	e := session.NewStateEntry(st{Title: "x"})
	assert.Equal(t, "x", e.State.Title)
	assert.Empty(t, e.ID)
}

func TestAsMessageEntry(t *testing.T) {
	e := session.NewMessageEntry(ai.UserMessage("hi"))
	got, ok := session.AsMessageEntry(e)
	require.True(t, ok)
	assert.Equal(t, "hi", got.Text())

	_, ok = session.AsMessageEntry(session.CompactionEntry{})
	assert.False(t, ok)
}

func TestFilter(t *testing.T) {
	entries := []session.Entry{
		session.NewMessageEntry(ai.UserMessage("a")),
		session.CompactionEntry{Summary: "s"},
		session.NewMessageEntry(ai.AssistantMessage(ai.Text{Text: "b"})),
	}
	assert.Len(t, session.Filter[session.MessageEntry](entries), 2)
	assert.Len(t, session.Filter[session.CompactionEntry](entries), 1)
	assert.Empty(t, session.Filter[session.MessageEntry](nil))
}

func TestLatestState(t *testing.T) {
	type st struct{ Title string }
	entries := []session.Entry{
		session.NewStateEntry(st{Title: "first"}),
		session.NewMessageEntry(ai.UserMessage("x")),
		session.NewStateEntry(st{Title: "second"}),
	}
	got, ok := session.LatestState[st](entries)
	require.True(t, ok)
	assert.Equal(t, "second", got.Title, "last StateEntry in append order wins")

	_, ok = session.LatestState[st](nil)
	assert.False(t, ok)
}
