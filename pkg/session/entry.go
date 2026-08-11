package session

import (
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
)

// EntryHeader carries the tree fields that every [Entry] shares. The
// ParentID pointer is the entire tree structure. The store stays a flat
// append-only log, and the tree is derived from it.
type EntryHeader struct {
	// ID identifies the entry in its session. It is assigned on append.
	ID string `json:"id"`

	// ParentID is the ID of the parent entry. An empty value means the
	// root entry.
	ParentID string `json:"parent_id,omitempty"`

	// CreatedAt is the time when the entry was appended.
	CreatedAt time.Time `json:"created_at"`
}

func (EntryHeader) entry() {}

// Header returns the tree fields.
func (h EntryHeader) Header() EntryHeader { return h }

// Entry is one node in a session's transcript tree. It is a
// [MessageEntry], a [CompactionEntry], or an application-defined entry
// that embeds [CustomEntry].
//
// The interface is sealed. External packages extend it by embedding
// [CustomEntry], not by implementing the markers directly.
type Entry interface {
	entry() // sealed marker
	Header() EntryHeader
}

// MessageEntry wraps an [ai.Message] as a tree node.
//
// Meta and Ephemeral both mark injected context, such as attachments,
// reminders, and skill bodies. The model reads this context, and
// transcript views hide it. Injected context is the mirror image of
// [CustomEntry], which the application sees but the model never reads.
// Meta and Ephemeral differ in one thing only: durability. A meta entry
// goes to the [Store] and comes back on resume. An ephemeral entry never
// leaves memory.
type MessageEntry struct {
	EntryHeader
	ai.Message

	// Meta marks injected context that persists. The model reads it, and
	// transcript views hide it.
	Meta bool

	// Ephemeral marks injected context that must not reach a [Store].
	// A durable agent keeps this context in its in-memory log and shows
	// it to the model for one run. The agent never writes it, so it is
	// gone on resume. Such context is true only at this moment, like a
	// reminder computed from live state. The codec never serializes this
	// field, so an entry read back from a [Store] is never ephemeral.
	Ephemeral bool
}

// NewMessageEntry wraps an [ai.Message] into a [MessageEntry].
// The append operation assigns the tree fields.
func NewMessageEntry(m ai.Message) MessageEntry {
	return MessageEntry{Message: m}
}

// CustomEntry is a base type that application-defined entries embed
// to satisfy the [Entry] interface. Custom entries persist in the
// transcript tree, but the model never reads them.
//
//	type ArtifactEntry struct {
//	    session.CustomEntry
//	    Title   string
//	    Content string
//	}
//
// To persist custom entries through a [Store], register the concrete
// type once with [RegisterCustom]. The decoder then rebuilds it.
type CustomEntry struct {
	EntryHeader

	// Kind is the application-defined discriminator.
	Kind string `json:"kind"`
}

// CompactionEntry summarizes the path before it. The durable agent
// appends one when it compacts, and the agent removes nothing. When it
// builds the model context, it uses the latest compaction entry on the
// active path. That entry becomes a summary, followed only by the
// entries from FirstKeptID forward.
type CompactionEntry struct {
	EntryHeader

	// Summary is the model-written digest of the compacted turns.
	Summary string

	// FirstKeptID is the ID of the earliest path entry kept verbatim.
	FirstKeptID string

	// TokensBefore is the approximate context size before compaction.
	TokensBefore int
}

// AsMessageEntry returns the [MessageEntry] in an [Entry]. The second
// result reports whether the entry is a [MessageEntry].
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
