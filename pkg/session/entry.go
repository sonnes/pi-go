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
	ParentID string `json:"parentId,omitempty"`

	// CreatedAt is when the entry was appended.
	CreatedAt time.Time `json:"createdAt"`
}

func (EntryHeader) entry() {}

// Header returns the tree fields.
func (h EntryHeader) Header() EntryHeader { return h }

// Entry is one node in a session's transcript tree. It is either a
// [MessageEntry], a [CompactionEntry], a [StateEntry], or an
// application-defined entry embedding [CustomEntry].
//
// The interface is sealed — external packages extend it by embedding
// [CustomEntry], not by implementing the markers directly.
type Entry interface {
	entry() // sealed marker
	Header() EntryHeader
}

// MessageEntry wraps an [ai.Message] as a tree node.
//
// Meta marks entries that are sent to the model but hidden from
// transcript views — attachments, injected context, reminders. It is
// the mirror image of [CustomEntry], which is visible to the
// application but never sent to the model.
type MessageEntry struct {
	EntryHeader
	ai.Message

	// Meta marks the entry model-visible but transcript-hidden.
	Meta bool
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

// StateEntry logs a change to a session's typed state T. It is the single
// entry type for every session-level change — title, active model, mode,
// or whatever the application models in T — instead of a separate entry
// type per field.
//
// The current state is the last StateEntry in append order (last-wins),
// so session state is a property of the session as a whole, not of a
// position in the tree: rewinding or branching does not revert it. Use
// [LatestState] to fold it from the log.
//
// StateEntry is model-invisible: it configures the session and is never
// sent to the model.
type StateEntry[T any] struct {
	EntryHeader

	// State is the session state as of this change.
	State T
}

// NewStateEntry wraps a session state value into a [StateEntry].
// Tree fields are assigned when the entry is appended.
func NewStateEntry[T any](state T) StateEntry[T] {
	return StateEntry[T]{State: state}
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

// LatestState folds the current session state from the log: the State of
// the last [StateEntry] of type T in append order, or (zero, false) if the
// session has logged none.
func LatestState[T any](entries []Entry) (T, bool) {
	var out T
	found := false
	for _, e := range entries {
		if se, ok := e.(StateEntry[T]); ok {
			out = se.State
			found = true
		}
	}
	return out, found
}
