// Package session provides the persistence primitives for durable agent
// conversations: the [Session] existence record, the [Entry] transcript
// tree, and the [Store] contract an application implements over its
// database.
//
// [Session] records that a conversation exists and where it was forked
// from. It carries no application state — metadata like titles, modes,
// or model choices belongs to the application's own storage, keyed by
// the session ID. [Store] is exactly the contract the durable agent
// consumes: create a session, load its entries, append to its log.
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
