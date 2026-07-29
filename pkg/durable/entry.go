package durable

import (
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
)

// Text returns a user text entry for [Agent.Run]. Tree fields are
// assigned when the entry is appended.
//
// Set Meta on the result to make it injected context the model reads
// and the transcript hides, kept across restarts — a skill body, the
// instructions a command expanded to. For the same thing without the
// durability, see [Ephemeral]:
//
//	body := durable.Text(skillMarkdown)
//	body.Meta = true
//	da.Run(ctx, body, durable.Text(input))
func Text(text string) session.MessageEntry {
	return session.NewMessageEntry(ai.UserMessage(text))
}

// Image returns a user entry carrying text and images, for
// [Agent.Run]. Tree fields are assigned when the entry is appended.
func Image(text string, images ...ai.Image) session.MessageEntry {
	return session.NewMessageEntry(ai.UserImageMessage(text, images...))
}

// File returns a user entry carrying text and file attachments, for
// [Agent.Run]. Tree fields are assigned when the entry is appended.
func File(text string, files ...ai.File) session.MessageEntry {
	return session.NewMessageEntry(ai.UserFileMessage(text, files...))
}

// Ephemeral marks an entry as injected context that [Agent.Run] sends
// to the model without persisting it. It is the non-durable half of a
// pair: set [session.MessageEntry.Meta] instead when the injected
// context should survive a restart.
//
// Use it for what is only true right now — a reminder computed from
// live state, a directory listing, the current diagnostics — where
// persisting would pin stale text into the session and replay it on
// every resume:
//
//	da.Run(ctx, durable.Ephemeral(durable.Text(reminder)), durable.Text(input))
//
// It composes with [Text], [Image], and [File]. The entry is recorded
// in the agent's in-memory log — [Agent.Entries] returns it, with an ID
// — but it is never written to the store, stays off the durable chain,
// and so is absent from [Agent.Transcript], from later runs, and from
// the session on resume.
func Ephemeral(e session.MessageEntry) session.MessageEntry {
	e.Ephemeral = true
	return e
}
