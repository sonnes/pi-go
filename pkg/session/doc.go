// Package session provides the persistence primitives for durable agent
// conversations: the [Session] identity and state, the [Entry] transcript
// tree, and the [Store] contract an application implements over its
// database.
//
// [Session] is generic over an application-defined state type T (a title,
// active model, mode, or any struct). Changes to that state are logged as
// [StateEntry] values, and the current value is the last one in append
// order — session state is a property of the whole session, not of a tree
// position.
//
// The transcript is an append-only tree. Every [Entry] embeds an
// [EntryHeader] carrying its ID, parent ID, and timestamp; the parent
// pointer is the entire tree structure, so the store stays a flat log
// and the tree is derived from it.
//
// Entry shapes: [MessageEntry] wraps an [ai.Message]; [CompactionEntry]
// summarizes earlier turns; [StateEntry] records a session-state change;
// and [CustomEntry] is an application-defined node that persists in the
// tree but is never sent to the model. Applications add their own shapes
// by embedding [CustomEntry].
//
// The agent loop that drives these types lives in pkg/durable.
package session
