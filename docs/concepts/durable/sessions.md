---
title: "Sessions"
summary: "How mutable session metadata stays separate from the append-only transcript"
read_when:
  - Deciding what a session ID should mean in your application
  - Implementing a Store over your own database
  - Understanding what survives a crash and what is repaired on resume
  - Deciding which session changes publish lifecycle events
---

# Sessions

A durable session has two persisted parts: a mutable `Session[T]` record and an append-only entry log. They share an ID but follow different mutation rules. `pkg/session` defines both parts; `pkg/durable` runs the conversation from the entry log.

## The session ID is the memory boundary

The SDK never invents meaning for a session ID. It is a string the application chooses, and choosing it is the real design decision — a user ID gives one continuous thread per person, a ticket number gives one per issue, a random ID gives a throwaway conversation.

Opening an agent with an ID that exists resumes it. Opening one with an unknown ID creates it. `New` makes that decision by calling `LoadEntries`: `ErrSessionNotFound` leads to `CreateSession`, and existing entries hydrate the transcript tree. There is no separate resume verb because both paths produce the same ready agent.

```go
da, err := durable.New[ChatState](ctx, model,
    durable.WithStore(store),
    durable.WithSessionID("user-42"),
)
```

Instances are not tracked. Two live agents on the same session each append from the leaf they loaded, so concurrent instances grow sibling branches rather than overwriting history.

## The agent owns transcript state, not application state

The durable agent keeps a session ID, the loaded entries, a tree index, and an in-memory leaf. It never calls `LoadSession` or caches `Session.State`. Call `SessionID` when `New` generated the ID for you.

This boundary prevents a long-lived agent from holding a stale copy of application metadata. It also keeps titles, modes, and model choices out of transcript control flow. The type parameter `T` binds the agent to the correct `Store[T]` and supplies the zero-valued record used when `New` creates a session; the agent does not otherwise interpret it.

| Concern | Owner | Mutation rule |
| ------- | ----- | ------------- |
| Session identity and application state | Application through `Store[T]` | Replace the record explicitly |
| Transcript entries | Durable agent through `Store[T]` | Append only |
| Active leaf | One live durable agent | Move in memory with `Branch` |
| Run progress and persistence receipts | The stream returned by `Run` | Scoped to one run |
| Lifecycle notifications | Durable agent through `Publisher` | Publish after commit |

## Update session metadata through the store

`Session[T]` is generic over whatever the application tracks per conversation: a title, the active model, a mode.

The session record is the source of truth for application metadata. Load it, change the snapshot, set `UpdatedAt` according to your application's policy, and replace it explicitly:

```go
sess, err := store.LoadSession(ctx, "user-42")
if err != nil {
    return err
}
sess.State.Title = "Refund request"
sess.UpdatedAt = time.Now()
if err := store.UpdateSession(ctx, sess); err != nil {
    return err
}
```

Entry appends never change session metadata, including `UpdatedAt`. This is intentional: transcript activity and application metadata have different write policies. If the application wants `UpdatedAt` to mean last conversation activity, it must update the session record at that boundary.

## The store preserves the ownership boundary

A `Store[T]` has five methods. `CreateSession`, `LoadSession`, and `UpdateSession` manage mutable records. `LoadEntries` and `AppendEntries` manage append-only transcript logs. The application owns the schema, encoding, and database behind that boundary.

Keeping the operations separate prevents an entry append from having hidden metadata effects. It also prevents metadata updates from becoming transcript nodes that alter parent chains, branches, or model context. Applications that need an auditable metadata change can append an application-defined `CustomEntry` explicitly.

The memory store keeps records and logs in separate maps. The filesystem store uses one directory per session:

```text
<root>/<session-id>/session.json
<root>/<session-id>/entries.jsonl
```

`CreateSession` initializes both files so the record and empty log appear together. Updating `session.json` never touches `entries.jsonl`, and appending entries never rewrites the session record. Entries come back in append order because the tree is derived from parent pointers and the leaf is recovered as the last appended entry.

Custom entry types need `session.RegisterCustom` once at init so a store can decode them back into their concrete Go type. Unregistered kinds decode to a bare `CustomEntry` — the header and kind survive, the application fields do not.

## Fork creates transcript identity, not copied metadata

`Fork` creates a child session with a new ID, the source session ID in `ParentID`, fresh timestamps, and zero-valued application state. It copies and re-chains only the source agent's active entry path.

The state starts empty because the durable agent never reads the source record. If the child should inherit a title, model, or other application state, load both records and update the child explicitly after the fork.

## Run events are persistence receipts

Run input is persisted before the loop starts. Every message the loop produces is persisted when its `message_end` arrives, and the event is forwarded only after that append succeeds, carrying the entries it wrote.

That ordering is what makes the run stream trustworthy. An event a consumer has seen is an event whose data is already in the store, so a UI that rendered a message never has to explain a message that vanished after a crash.

The cost is a narrow window with a known repair. A crash between an assistant message landing and its tool results landing leaves a tool call with no result, which providers reject. Resume repairs it by synthesizing interrupted tool results for the orphaned calls, so a session that died mid-tool reopens into a valid conversation rather than an unusable one.

## Lifecycle mutations use a publisher

`New`, `Branch`, `Fork`, and `Compact` change session lifecycle state outside a run. Configure `WithPublisher` to receive `session_init`, `session_branched`, `session_forked`, and `session_compacted` after those effects succeed.

The publisher stays separate from the run stream because these mutations do not belong to a turn. Delivery is synchronous and happens outside the agent's locks, so a publisher may inspect the agent or hand the event to another system. Keep `Publish` fast because it runs on the mutating call's path.

Forked agents inherit the source publisher. A successful fork publishes `session_forked` for the source and then `session_init` for the child. A failed fork publishes neither event.

Session metadata updates do not publish. The application calls `UpdateSession` directly, and the durable agent neither performs nor observes that operation. This keeps the notification boundary aligned with the state the agent actually owns.

See [Durable Events](events.md) for event fields, ordering, delivery semantics, and a publisher example.

## Related

- [Entries](entries.md) — entry visibility and persistence
- [Transcript Tree](tree.md) — the leaf pointer, branching, forking, compaction
- [Durable Events](events.md) — run receipts and lifecycle notifications
- [Agent](../agent/agent.md) — the loop a durable agent wraps
- [Streaming](../agent/streaming.md) — the run stream whose events carry the receipts
