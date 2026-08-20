---
title: "Durable Events"
summary: "When to consume run-stream receipts and when to publish session lifecycle changes"
read_when:
  - Wiring durable-agent activity to a UI, log, or event bus
  - Choosing between the run stream and WithPublisher
  - Handling initialization, branch, fork, or compaction notifications
---

# Durable Events

Durable agents expose two event channels because a run and a session have different lifetimes. The stream returned by `Run` reports progress within one turn. A `Publisher` reports lifecycle changes that happen outside a run.

## Choose the channel by the lifetime of the change

| Channel | Scope | Carries | Use it for |
| ------- | ----- | ------- | ---------- |
| `Run(...).Events()` | One run | Lifted `agent.Event` values and persistence receipts | Rendering model and tool progress for the active turn |
| `Publisher.Publish` | Session lifecycle | Initialization, branch, fork, and compaction events | Notifying application infrastructure about agent-owned mutations |

The split prevents lifecycle changes from waiting for a later run before consumers can observe them. It also keeps a run stream self-contained: draining one stream tells you only what happened during that run.

## Run events double as persistence receipts

Every event on the run stream has type `EventAgent` and carries the original event in `Event.Agent`. Most events only lift inner-agent progress. Two boundaries add durability information:

- `agent_start` carries the durable input entries and the resulting `LeafID`.
- `message_end` carries the completed message entry and the resulting `LeafID`.

The durable agent forwards each boundary only after `AppendEntries` succeeds. A consumer that receives the event can therefore treat its entries as persisted. Ephemeral entries never appear in a receipt because the store never receives them.

Lifecycle events never appear on this stream. Consume them through `WithPublisher` instead.

## Configure one lifecycle publisher

`WithPublisher` accepts a `Publisher` or `PublisherFunc`. The same publisher is inherited by every agent created through `Fork`.

```go
publisher := durable.PublisherFunc(func(event durable.Event) {
    switch event.Type {
    case durable.EventSessionInit:
        log.Printf("session %s ready at %s", event.SessionID, event.LeafID)
    case durable.EventSessionBranched:
        log.Printf("leaf moved from %s to %s", event.FromID, event.LeafID)
    case durable.EventSessionForked:
        log.Printf("session %s forked from %s", event.SessionID, event.ParentID)
    case durable.EventSessionCompacted:
        log.Printf("compacted at %s", event.LeafID)
    }
})

da, err := durable.New(ctx, durable.Model(lm),
    durable.WithStore(store),
    durable.WithSessionID("user-42"),
    durable.WithPublisher(publisher),
)
```

The publisher receives only successful lifecycle effects. `Event.Agent` is nil for these events, and fields not listed below remain zero-valued.

| Event | Published after | Relevant fields |
| ----- | --------------- | --------------- |
| `EventSessionInit` | `New` creates or resumes a ready agent | `SessionID`; `LeafID` is empty when fresh and the resume leaf otherwise |
| `EventSessionBranched` | `Branch` moves the in-memory leaf | `FromID`, `LeafID` |
| `EventSessionForked` | `Fork` creates the child and copies its active path | `SessionID` for the child, `ParentID` for the source |
| `EventSessionCompacted` | `Compact` appends the compaction entry | `Entries` with one `CompactionEntry`, `LeafID` |

A successful fork publishes `EventSessionForked` for the source and then `EventSessionInit` for the child. Both calls go to the inherited publisher. A failed operation publishes nothing, and a compaction that has nothing to summarize publishes nothing.

## Delivery stays application-owned

`Publish` runs synchronously after the effect succeeds and outside the agent's locks. Keep the callback fast or hand the event to a queue that your application owns. If callers mutate agents concurrently, the publisher must also be safe for concurrent calls.

The package does not buffer, retry, persist, or fan out lifecycle events. Those policies belong to the application because delivery requirements vary independently from transcript persistence. Without `WithPublisher`, lifecycle events are discarded and agent behavior is unchanged.

## Application metadata changes do not publish

The durable agent owns the session ID, transcript tree, and active leaf. Application metadata lives in the application's own storage and never passes through the agent or its store, so the agent cannot observe a metadata change and does not publish an update event.

If metadata changes need notifications, emit them in the application service that performs them. This keeps the publisher aligned with mutations the durable agent actually performs and avoids rebuilding a hidden session-state channel.

## Related

- [Sessions](sessions.md) — ownership of session records and transcript history
- [Transcript Tree](tree.md) — how branch, fork, and compaction change the active path
- [Entries](entries.md) — which values are durable enough to appear in receipts
- [Agent Streaming](../agent/streaming.md) — the inner event lifecycle lifted onto each run stream
