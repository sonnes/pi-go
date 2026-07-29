---
title: "Sessions"
summary: "Session identity, typed application state, the Store contract, and what resume rebuilds"
read_when:
  - Deciding what a session ID should mean in your application
  - Implementing a Store over your own database
  - Understanding what survives a crash and what is repaired on resume
---

# Sessions

A session is the durable identity of one conversation: an ID, timestamps, an application-defined state value, and an append-only log of entries. `pkg/session` holds those primitives; `pkg/durable` runs an agent loop against them.

## The session ID is the memory boundary

The SDK never invents meaning for a session ID. It is a string the application chooses, and choosing it is the real design decision — a user ID gives one continuous thread per person, a ticket number gives one per issue, a random ID gives a throwaway conversation.

Opening an agent with an ID that exists resumes it. Opening one with an ID that does not creates it. There is no separate "resume" verb, because there is no state in which the distinction matters to the caller.

```go
da, err := durable.New[ChatState](ctx, model,
    durable.WithStore(store),
    durable.WithSessionID("user-42"),
)
```

Instances are not tracked. Two live agents on the same session cannot corrupt it — each appends from its own leaf, so concurrent instances grow sibling branches rather than fighting over one.

## State is a fold over the log, not a field you mutate

`Session[T]` is generic over whatever the application tracks per conversation: a title, the active model, a mode.

The current value is a snapshot, but it is not the source of truth. Every change is appended as a `StateEntry`, and the current state is the last one in append order. That makes state a property of the session as a whole rather than of a position in the tree, with two consequences worth internalizing: the change history is auditable, and rewinding the transcript does not revert state. A title set at turn 30 is still the title after branching back to turn 3.

`session.LatestState` performs the fold if you are reading a raw log yourself.

## Design: the store contract is three methods

A `Store[T]` creates a session, loads one with its entries in append order, and appends entries. That is the whole interface, and the narrowness is the point — the application owns the schema, the encoding, and the database.

The SDK ships a memory store and a filesystem store that writes JSONL. Neither is privileged; both implement the same three methods you would.

Two properties the contract requires. Writes are append-only, so a store never rewrites or deletes history. And entries come back in the order they went in, because the tree is derived from parent pointers and the leaf is recovered as the last appended entry.

Custom entry types need `session.RegisterCustom` once at init so a store can decode them back into their concrete Go type. Unregistered kinds decode to a bare `CustomEntry` — the header and kind survive, the application fields do not.

## Design: persistence is per message, and events are receipts

Run input is persisted before the loop starts. Every message the loop produces is persisted when its `message_end` arrives, and the event is forwarded only after that append succeeds, carrying the entries it wrote.

That ordering is what makes the run stream trustworthy. An event a consumer has seen is an event whose data is already in the store, so a UI that rendered a message never has to explain a message that vanished after a crash.

The cost is a narrow window with a known repair. A crash between an assistant message landing and its tool results landing leaves a tool call with no result, which providers reject. Resume repairs it by synthesizing interrupted tool results for the orphaned calls, so a session that died mid-tool reopens into a valid conversation rather than an unusable one.

## Design: session events have no broker

Mutations that are not part of a run — init, state changes, branches, forks, compaction — are delivered to a `Publisher` supplied at construction, at the moment the mutation commits.

The application owns delivery entirely: forward to a websocket, fan out to subscribers, or drop it. Without a publisher the events are discarded and nothing degrades. The SDK ships no queue, no subscriber registry, and no delivery guarantee, because every application that needs one already has one.

## Related

- [Entries](/concepts/durable/entries) — the four kinds of entry, and the views that read them
- [Transcript Tree](/concepts/durable/tree) — the leaf pointer, branching, forking, compaction
- [Agent](/concepts/agent/agent) — the loop a durable agent wraps
- [Streaming](/concepts/agent/streaming) — the run stream whose events carry the receipts
