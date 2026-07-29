---
title: "Agent History"
summary: "How the loop holds conversation history, and the two places a hook can change it"
read_when:
  - Seeding or inspecting an agent's conversation history
  - Changing what the model sees without changing what the agent stores
  - Looking for application-defined message types
---

# Agent History

An agent's history is a plain `[]ai.Message`. There is no agent-level message wrapper: what the loop stores is what the provider is sent, in the same type documented under [ai.Message](/concepts/ai/messages).

## The lifecycle of the slice

`WithHistory` seeds it at construction, and the loop copies what you pass — the agent never aliases your slice. From there the loop owns it: `Run` appends its input messages, then appends every message the turn produces, assistant messages and tool results alike. `Messages()` hands back a copy, so reading history while a run is in flight is safe and gives you a stable snapshot.

Nothing is removed. A turn only ever grows the slice, which is what makes the history a faithful record of what the model was told.

## Design: what the model sees is derived, not stored

Two different questions look similar and are not: what the agent *has*, and what the model *gets* on the next call. Keeping them separate is why hooks exist at two points rather than one.

`HookBeforeCall` runs while the prompt is being built. Whatever it returns becomes the `Messages` on that request, and the stored history is untouched. This is the hook for anything the model should see once — an injected reminder, a pruned window over a long conversation, a summarized prefix. The next call starts from the unmodified history again, so a filter here never compounds.

`HookAfterTurn` is the one that rewrites what the agent holds. Returning a slice from it replaces the history outright, which is how compaction and steering are built. Use it when the change should persist into every later turn; use `HookBeforeCall` when it should not.

`HookBeforeStop` neither reads nor rewrites history — it injects follow-up messages that continue a loop that was about to end.

## Application-defined records

The agent layer has no custom message type, and deliberately so. `ai.Message` maps to provider wire formats, and widening it to carry artifacts, UI state, or status records would put application concerns into the type whose shape the provider dictates.

Those records belong to a session instead. `pkg/session` defines an entry tree where `CustomEntry` is exactly this: an application-defined node that persists in the transcript and is never sent to the model. Reaching it means running the loop through [pkg/durable](/concepts/durable/entries), which takes entries rather than messages at its input boundary and so can carry both kinds in a single turn.

If you only need something in front of the model for one call and nowhere else, you do not need any of that — `HookBeforeCall` is enough.

## Related

- [ai.Message](/concepts/ai/messages) — the message type the history holds
- [Durable Entries](/concepts/durable/entries) — the entry model, including application-defined and non-persisted kinds
- [Agent](/concepts/agent/agent) — the loop that owns the history
- [Agent State](/concepts/agent/agent-state) — runtime state observability
