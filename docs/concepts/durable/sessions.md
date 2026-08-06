---
title: "Sessions"
summary: "What the session record is, what the store owns, and where application metadata lives"
read_when:
  - Deciding what a session ID should mean in your application
  - Implementing a Store over your own database
  - Understanding what survives a crash and what is repaired on resume
  - Deciding which session changes publish lifecycle events
---

# Sessions

A durable session has two persisted parts: a `Session` record that marks existence and lineage, and an append-only entry log. `pkg/session` defines both; `pkg/durable` runs the conversation from the entry log.

## The session ID is the memory boundary

The SDK never invents meaning for a session ID. It is a string the application chooses, and choosing it is the real design decision — a user ID gives one continuous thread per person, a ticket number gives one per issue, a random ID gives a throwaway conversation.

Opening an agent with an ID that exists resumes it. Opening one with an unknown ID creates it. `New` makes that decision by calling `LoadEntries`: `ErrSessionNotFound` leads to `CreateSession`, and existing entries hydrate the transcript tree. There is no separate resume verb because both paths produce the same ready agent.

```go
da, err := durable.New(ctx, model,
    durable.WithStore(store),
    durable.WithSessionID("user-42"),
)
```

Instances are not tracked. Two live agents on the same session each append from the leaf they loaded, so concurrent instances grow sibling branches rather than overwriting history.

## The agent owns transcript state, not application state

The durable agent keeps a session ID, the loaded entries, a tree index, and an in-memory leaf. Call `SessionID` when `New` generated the ID for you.

| Concern | Owner | Mutation rule |
| ------- | ----- | ------------- |
| Session existence and fork lineage | Store, written by `CreateSession` | Written once, never updated |
| Application metadata (title, mode, model) | Application's own storage, keyed by session ID | Never passes through the SDK |
| Transcript entries | Durable agent through `Store` | Append only |
| Active leaf | One live durable agent | Move in memory with `Branch` |
| Run progress and persistence receipts | The stream returned by `Run` | Scoped to one run |
| Lifecycle notifications | Durable agent through `Publisher` | Publish after commit |

## Application metadata stays in the application

The session record carries no application state — no title, no mode, no model choice. Whatever the application tracks per conversation lives in its own storage, keyed by the session ID, with whatever schema and write policy it wants.

Design: an earlier revision made `Session[T]` generic over an application state type, with `LoadSession`/`UpdateSession` in the store contract. The durable agent never read any of it — it creates the record and runs entirely off the entry log — yet the type parameter spread to every SDK signature (`Store[T]`, `Agent[T]`, `New[T]`, `WithStore[T]`) and forced a runtime type assertion where options met construction. Interfaces belong to their consumers: `Store` now contains exactly the three operations the durable agent performs, and application metadata never has to fit an SDK-imposed shape. The concrete stores still expose `LoadSession` for reading back existence and lineage; it is simply not part of the contract a custom store must implement.

A useful side effect of the boundary: transcript activity and application metadata cannot have hidden effects on each other. Appending entries never touches metadata, and a metadata write in the application can never alter parent chains, branches, or model context. An application that wants an auditable metadata change in the conversation itself can append its own `CustomEntry` explicitly.

## The store preserves the ownership boundary

A `Store` has three methods — `CreateSession` registers existence and lineage, `LoadEntries` and `AppendEntries` manage the append-only transcript log. The application owns the schema, encoding, and database behind that boundary.

The memory store keeps records and logs in separate maps. The filesystem store uses one append-only JSONL file per session, whose first line is a `session_init` event carrying the record:

```text
<root>/<session-id>.jsonl
  {"type":"session_init","id":...,"parent_id":...,"created_at":...}
  {"type":"message",...}
  ...
```

`CreateSession` writes the `session_init` line; everything after it is appended and nothing is ever rewritten — the file only grows. Every line carries a type discriminator, so the log reads as one uniform event stream with the session's creation in-band as its first event. Entries come back in append order because the tree is derived from parent pointers and the leaf is recovered as the last appended entry.

Custom entry types need `session.RegisterCustom` once at init so a store can decode them back into their concrete Go type. Unregistered kinds decode to a bare `CustomEntry` — the header and kind survive, the application fields do not.

## Fork creates transcript identity

`Fork` creates a child session with a new ID and the source session ID as its `ParentID`, then copies and re-chains only the source agent's active entry path. Any application metadata the child should inherit is the application's to copy, in its own storage.

## Run events are persistence receipts

Run input is persisted before the loop starts. Every message the loop produces is persisted when its `message_end` arrives, and the event is forwarded only after that append succeeds, carrying the entries it wrote.

That ordering is what makes the run stream trustworthy. An event a consumer has seen is an event whose data is already in the store, so a UI that rendered a message never has to explain a message that vanished after a crash.

The cost is a narrow window with a known repair. A crash between an assistant message landing and its tool results landing leaves a tool call with no result, which providers reject. Resume repairs it by synthesizing interrupted tool results for the orphaned calls, so a session that died mid-tool reopens into a valid conversation rather than an unusable one.

## Lifecycle mutations use a publisher

`New`, `Branch`, `Fork`, and `Compact` change session lifecycle state outside a run. Configure `WithPublisher` to receive `session_init`, `session_branched`, `session_forked`, and `session_compacted` after those effects succeed.

The publisher stays separate from the run stream because these mutations do not belong to a turn. Delivery is synchronous and happens outside the agent's locks, so a publisher may inspect the agent or hand the event to another system. Keep `Publish` fast because it runs on the mutating call's path.

Forked agents inherit the source publisher. A successful fork publishes `session_forked` for the source and then `session_init` for the child. A failed fork publishes neither event.

Application metadata changes do not publish: they never pass through the agent or its store, so there is nothing for the SDK to observe. If metadata changes need notifications, emit them in the application service that performs them.

See [Durable Events](events.md) for event fields, ordering, delivery semantics, and a publisher example.

## Related

- [Entries](entries.md) — entry visibility and persistence
- [Transcript Tree](tree.md) — the leaf pointer, branching, forking, compaction
- [Durable Events](events.md) — run receipts and lifecycle notifications
- [Agent](../agent/agent.md) — the loop a durable agent wraps
- [Streaming](../agent/streaming.md) — the run stream whose events carry the receipts
