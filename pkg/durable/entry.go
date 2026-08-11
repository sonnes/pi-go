package durable

import (
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
)

// Text returns a user text entry for [Agent.Run]. [Agent.Run] assigns
// the tree fields when it appends the entry.
//
// Set Meta on the result to make the entry injected context. The model
// reads injected context, and the transcript hides it. A meta entry
// survives restarts. Two examples are a skill body and the instructions
// that a command expanded to. For the same thing without the
// durability, see [Ephemeral]:
//
//	body := durable.Text(skillMarkdown)
//	body.Meta = true
//	da.Run(ctx, body, durable.Text(input))
func Text(text string) session.MessageEntry {
	return session.NewMessageEntry(ai.UserMessage(text))
}

// Image returns a user entry that carries text and images, for
// [Agent.Run]. [Agent.Run] assigns the tree fields when it appends the
// entry.
func Image(text string, images ...ai.Image) session.MessageEntry {
	return session.NewMessageEntry(ai.UserImageMessage(text, images...))
}

// File returns a user entry that carries text and file attachments, for
// [Agent.Run]. [Agent.Run] assigns the tree fields when it appends the
// entry.
func File(text string, files ...ai.File) session.MessageEntry {
	return session.NewMessageEntry(ai.UserFileMessage(text, files...))
}

// Ephemeral marks an entry as injected context. [Agent.Run] sends the
// entry to the model without persistence. Ephemeral is the non-durable
// half of a pair. If the injected context must survive a restart, set
// [session.MessageEntry.Meta] instead.
//
// Use Ephemeral for what is true only right now: a reminder computed
// from live state, a directory listing, or the current diagnostics.
// Persistence would pin stale text into the session and replay it on
// every resume:
//
//	da.Run(ctx, durable.Ephemeral(durable.Text(reminder)), durable.Text(input))
//
// Ephemeral composes with [Text], [Image], and [File]. The agent
// records the entry in its in-memory log, and [Agent.Entries] returns
// it with an ID. The agent never writes the entry to the store, and the
// entry stays off the durable chain. The entry is therefore absent from
// [Agent.Transcript], from later runs, and from the session on resume.
func Ephemeral(e session.MessageEntry) session.MessageEntry {
	e.Ephemeral = true
	return e
}
