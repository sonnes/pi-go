---
title: "Transcript Tree"
summary: "One append-only tree and a leaf pointer, from which edit, retry, rewind, fork, and compaction all follow"
read_when:
  - Implementing edit, retry, or rewind on a conversation
  - Exploring an alternative direction without losing the original
  - Shrinking a conversation that outgrew the context window
---

# Transcript Tree

The transcript is an append-only tree. Every entry carries its parent's ID, and that pointer is the entire structure — the store stays a flat log, and the tree is derived from it by walking parents.

A single piece of mutable state sits on top: the leaf, the entry the next turn will attach to. Nearly every conversation operation is a statement about the leaf, which is why the API has fewer verbs than the feature list suggests.

## Why a tree and not a list

The obvious model for a conversation is a list you append to. It fails at the first edit. A user who rewrites their last message wants the answer that follows from the new text, and a list can only give it by destroying the old branch — which loses the thing you would want when the retry turns out worse than the original.

A tree keeps both. Rewriting is not a deletion; it is a second child of the same parent. The abandoned branch stays in the store, addressable and complete, and nothing in the log is ever mutated or removed.

The active path is the walk from the leaf back to the root, reversed. Everything the model and the reader see is a projection of that path, so moving the leaf changes the conversation without touching a single stored entry.

## Branch is the only rewind primitive

`Branch` moves the leaf to an earlier entry. That is all it does, and it is enough for a family of features that usually get separate implementations:

- **Edit** — branch to the entry before the message being replaced, then run with the new text.
- **Retry** — branch to the same point and run again.
- **Rewind** — branch and stop.

A checkpoint, in this model, is just an entry ID you remembered. Capture `LeafID()` before a risky turn and pass it back to `Branch` to undo everything since.

```go
checkpoint := da.LeafID()
// ... a turn that may go badly ...
_ = da.Branch(ctx, checkpoint)
```

The leaf lives in memory. Reopening a session resumes at the last appended entry, not at wherever you had branched to — a rewind is a property of a working instance, not a stored fact about the conversation.

## Fork copies the path into a new session

Branching explores an alternative inside one session. `Fork` lifts the active path into a separate session, re-chained with fresh entry IDs, recording the original as its parent.

Reach for it when the alternative should have its own identity — a what-if the user might come back to, a sub-conversation with its own lifetime — rather than a sibling branch that shares the original's ID. The child inherits the publisher, so its events flow where the parent's do.

## Compaction summarizes without deleting

When a conversation outgrows the context window, `Compact` appends a `CompactionEntry` holding a model-written summary of the earlier turns and the ID of the first entry kept verbatim.

Nothing is removed, which is the property worth understanding. The summary changes only how the path is *projected*: the model view emits the summary as a user message, elides everything before the kept point, and keeps the rest as written. The full history is still in the tree, still on the path, and still rewindable — branching to a pre-compaction entry gives you the uncompacted conversation back.

`KeepTurns` controls how many recent turns stay verbatim, and `CompactPrompt` overrides the instruction given to the summarizer. The summary itself is written by a throwaway agent built from the same model and options as the session, so compaction needs no separately configured model. Custom entries pass through untouched.

## Related

- [Entries](/concepts/durable/entries) — what the nodes are, and which of them the model and reader each see
- [Sessions](/concepts/durable/sessions) — identity, state, and the store the tree is written to
- [Agent State](/concepts/agent/agent-state) — reading state during and after a run
