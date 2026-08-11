// Package session provides the persistence primitives for durable agent
// conversations. These are the [Session] existence record, the [Entry]
// transcript tree, and the [Store] contract that an application
// implements over its database.
//
// [Session] records that a conversation exists and where it was forked
// from. It carries no application state. Metadata such as titles, modes,
// or model choices belongs to the application's own storage, keyed by
// the session ID. [Store] is exactly the contract that the durable agent
// consumes: create a session, load its entries, append to its log.
//
// The transcript is an append-only tree. Every [Entry] embeds an
// [EntryHeader] that carries its ID, parent ID, and timestamp. The
// parent pointer is the entire tree structure. The store therefore stays
// a flat log, and the tree is derived from it.
//
// There are three entry shapes. [MessageEntry] wraps an [ai.Message].
// [CompactionEntry] summarizes earlier turns. [CustomEntry] is an
// application-defined node that persists in the tree but never reaches
// the model. Applications add their own shapes by embedding
// [CustomEntry].
//
// The agent loop that drives these types lives in pkg/durable.
package session
