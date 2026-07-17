package session_test

import (
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codecState is the shared example session state for session-package tests.
type codecState struct {
	Title string
	Model string
}

// ArtifactEntry is an application-defined custom entry used to test the
// custom-entry codec path.
type ArtifactEntry struct {
	session.CustomEntry
	Title   string
	Content string
}

func init() {
	session.RegisterCustom("artifact", ArtifactEntry{})
}

func roundTrip[T any](t *testing.T, e session.Entry) session.Entry {
	t.Helper()
	data, err := session.MarshalEntry[T](e)
	require.NoError(t, err)
	got, err := session.UnmarshalEntry[T](data)
	require.NoError(t, err)
	return got
}

var codecTS = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestCodecMessageEntry(t *testing.T) {
	msg := ai.UserMessage("hello")
	msg.Timestamp = codecTS
	orig := session.MessageEntry{
		EntryHeader: session.EntryHeader{ID: "m1", ParentID: "p", CreatedAt: codecTS},
		Message:     msg,
		Meta:        true,
	}

	got := roundTrip[codecState](t, orig)
	me, ok := got.(session.MessageEntry)
	require.True(t, ok)
	assert.Equal(t, orig.EntryHeader, me.EntryHeader)
	assert.Equal(t, ai.RoleUser, me.Role)
	assert.Equal(t, "hello", me.Text())
	assert.True(t, me.Meta)
}

func TestCodecCompactionEntry(t *testing.T) {
	orig := session.CompactionEntry{
		EntryHeader:  session.EntryHeader{ID: "c1", CreatedAt: codecTS},
		Summary:      "sum",
		FirstKeptID:  "k1",
		TokensBefore: 42,
	}
	got := roundTrip[codecState](t, orig)
	assert.Equal(t, orig, got)
}

func TestCodecStateEntry(t *testing.T) {
	orig := session.StateEntry[codecState]{
		EntryHeader: session.EntryHeader{ID: "s1", CreatedAt: codecTS},
		State:       codecState{Title: "T", Model: "M"},
	}
	got := roundTrip[codecState](t, orig)
	assert.Equal(t, orig, got)
}

func TestCodecCustomEntryRegistered(t *testing.T) {
	orig := ArtifactEntry{
		CustomEntry: session.CustomEntry{
			EntryHeader: session.EntryHeader{ID: "a1", CreatedAt: codecTS},
			Kind:        "artifact",
		},
		Title:   "draft",
		Content: "# Proposal",
	}
	got := roundTrip[codecState](t, orig)
	ae, ok := got.(ArtifactEntry)
	require.True(t, ok, "registered custom entry decodes to its concrete type")
	assert.Equal(t, orig, ae)
}

func TestCodecCustomEntryUnregistered(t *testing.T) {
	orig := ArtifactEntry{
		CustomEntry: session.CustomEntry{
			EntryHeader: session.EntryHeader{ID: "a2", CreatedAt: codecTS},
			Kind:        "not-registered",
		},
		Title: "draft",
	}
	got := roundTrip[codecState](t, orig)
	ce, ok := got.(session.CustomEntry)
	require.True(t, ok, "unregistered kind falls back to a bare CustomEntry")
	assert.Equal(t, "not-registered", ce.Kind)
	assert.Equal(t, "a2", ce.ID)
}

func TestCodecUnknownType(t *testing.T) {
	_, err := session.UnmarshalEntry[codecState]([]byte(`{"type":"bogus","id":"x"}`))
	assert.Error(t, err)
}
