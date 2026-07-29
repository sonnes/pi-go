package durable_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/session"
)

func TestEntryConstructors(t *testing.T) {
	img := ai.Image{Data: "aGk=", MimeType: "image/png"}
	doc := ai.File{Data: "aGk=", MimeType: "application/pdf", Filename: "spec.pdf"}

	tests := []struct {
		name    string
		entry   session.MessageEntry
		content []ai.Content
	}{
		{
			name:    "text",
			entry:   durable.Text("hi"),
			content: []ai.Content{ai.Text{Text: "hi"}},
		},
		{
			name:    "image",
			entry:   durable.Image("look", img),
			content: []ai.Content{ai.Text{Text: "look"}, img},
		},
		{
			name:    "file",
			entry:   durable.File("read", doc),
			content: []ai.Content{ai.Text{Text: "read"}, doc},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, ai.RoleUser, tc.entry.Message.Role)
			assert.Equal(t, tc.content, tc.entry.Message.Content)
			assert.False(t, tc.entry.Meta)
			assert.False(t, tc.entry.Ephemeral)

			// Tree fields are assigned on append, not by the constructor.
			assert.Empty(t, tc.entry.Header().ID)
			assert.Empty(t, tc.entry.Header().ParentID)

			// The concrete type still satisfies the Run signature.
			var e session.Entry = tc.entry
			require.NotNil(t, e)
		})
	}
}

func TestEphemeral(t *testing.T) {
	e := durable.Ephemeral(durable.Text("hi"))

	assert.True(t, e.Ephemeral)
	assert.Equal(t, "hi", e.Message.Text())

	// Meta is the persisted sibling, so the two are independent flags.
	assert.False(t, e.Meta)

	// Both are injected context, so neither shows in a transcript view.
	assert.Empty(t, session.TranscriptView([]session.Entry{e}))
}
