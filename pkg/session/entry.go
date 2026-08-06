package session

import (
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
)

// EntryHeader carries the tree fields shared by every [Entry]. The
// ParentID pointer is the entire tree structure — the store stays a
// flat append-only log and the tree is derived from it.
type EntryHeader struct {
	// ID identifies the entry within its session. Assigned on append.
	ID string `json:"id"`

	// ParentID is the ID of the parent entry. Empty means root.
	ParentID string `json:"parent_id,omitempty"`

	// CreatedAt is when the entry was appended.
	CreatedAt time.Time `json:"created_at"`
}

func (EntryHeader) entry() {}

// Header returns the tree fields.
func (h EntryHeader) Header() EntryHeader { return h }

// Entry is one node in a session's transcript tree. It is either a
// [MessageEntry], a [CompactionEntry], or an application-defined entry
// embedding [CustomEntry].
//
// The interface is sealed — external packages extend it by embedding
// [CustomEntry], not by implementing the markers directly.
type Entry interface {
	entry() // sealed marker
	Header() EntryHeader
}

// MessageEntry wraps an [ai.Message] as a tree node.
//
// Meta and Ephemeral both mark injected context — attachments,
// reminders, skill bodies: sent to the model, hidden from transcript
// views. They are the mirror image of [CustomEntry], which is visible
// to the application but never sent to the model. The two differ in one
// thing, durability: a meta entry is written to the [Store] and comes
// back on resume, an ephemeral one never leaves memory.
type MessageEntry struct {
	EntryHeader
	ai.Message

	// Meta marks persisted injected context: model-visible,
	// transcript-hidden, and durable.
	Meta bool

	// Ephemeral marks injected context that must not reach a [Store].
	// A durable agent keeps it in its in-memory log and shows it to the
	// model for one run, but never writes it, so it is gone on resume —
	// context that is only true right now, like a reminder computed
	// from live state. It is never serialized, so an entry read back
	// from a [Store] is by definition not ephemeral.
	Ephemeral bool
}

// NewMessageEntry wraps an [ai.Message] into a [MessageEntry].
// Tree fields are assigned when the entry is appended.
func NewMessageEntry(m ai.Message) MessageEntry {
	return MessageEntry{Message: m}
}

// CustomEntry is a base type that application-defined entries embed
// to satisfy the [Entry] interface. Custom entries persist in the
// transcript tree but are never sent to the model.
//
//	type ArtifactEntry struct {
//	    session.CustomEntry
//	    Title   string
//	    Content string
//	}
//
// To persist custom entries through a [Store], register the concrete
// type once with [RegisterCustom] so it can be decoded.
type CustomEntry struct {
	EntryHeader

	// Kind is the application-defined discriminator.
	Kind string `json:"kind"`
}

// CompactionEntry summarizes the path before it. Appended when the
// durable agent compacts; nothing is deleted. When building the model
// context, the latest compaction entry on the active path is emitted
// as a summary followed only by entries from FirstKeptID forward.
type CompactionEntry struct {
	EntryHeader

	// Summary is the model-written digest of the compacted turns.
	Summary string

	// FirstKeptID is the earliest path entry kept verbatim.
	FirstKeptID string

	// TokensBefore is the approximate context size before compaction.
	TokensBefore int
}

// AsMessageEntry extracts the [MessageEntry] from an [Entry], if it
// is one.
func AsMessageEntry(e Entry) (MessageEntry, bool) {
	m, ok := e.(MessageEntry)
	return m, ok
}

// Filter returns all entries that match the concrete type T.
func Filter[T Entry](entries []Entry) []T {
	var out []T
	for _, e := range entries {
		if t, ok := e.(T); ok {
			out = append(out, t)
		}
	}
	return out
}
