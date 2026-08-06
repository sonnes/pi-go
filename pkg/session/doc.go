// Package session provides the persistence primitives for durable agent
// conversations: the [Session] identity and state, the [Entry] transcript
// tree, and the [Store] contract an application implements over its
// database.
//
// [Session] is generic over an application-defined state type T (a title,
// active model, mode, or any struct). The record is mutable through
// [Store.UpdateSession] and stored independently of its append-only entry
// log. This keeps application metadata out of transcript parent chains
// and lets a durable agent run without caching application state.
//
// The transcript is an append-only tree. Every [Entry] embeds an
// [EntryHeader] carrying its ID, parent ID, and timestamp; the parent
// pointer is the entire tree structure, so the store stays a flat log
// and the tree is derived from it.
//
// Entry shapes: [MessageEntry] wraps an [ai.Message]; [CompactionEntry]
// summarizes earlier turns; and [CustomEntry] is an application-defined
// node that persists in the tree but is never sent to the model.
// Applications add their own shapes by embedding [CustomEntry].
//
// The agent loop that drives these types lives in pkg/durable.
package session
