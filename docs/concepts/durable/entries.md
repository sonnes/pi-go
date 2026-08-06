---
title: "Entries"
summary: "How entry shapes control model visibility, transcript visibility, and persistence"
read_when:
  - Deciding how to inject a reminder, skill body, or artifact into a turn
  - Working out why something reached the model but not the transcript
  - Implementing an application-defined entry type
---

# Entries

A durable agent's unit of input is a `session.Entry`, not an `ai.Message`. That is the one shape difference from the plain loop, and everything on this page follows from it.

## Why entries

A conversation that survives restarts has more in it than what the user typed. A reminder computed from live state, an expanded slash command, a rendered artifact the UI wants to show — all of these belong to the turn, and each answers two questions differently: does the model see it, and does the store keep it.

A message can't express that. It has content and a role, and no room for either answer. So `Run` takes entries, the same currency `Append`, `Entries`, `Transcript`, and the persistence receipts already speak:

```go
da.Run(ctx, durable.Text("What changed since Friday?"))
```

`durable.Text`, `durable.Image`, and `durable.File` build the ordinary user entry, mirroring the `ai.UserMessage` constructors one for one.

## Entry shapes and input behavior

The `Entry` interface has three public shapes: `MessageEntry`, `CompactionEntry`, and application types that embed `CustomEntry`. Meta and ephemeral inputs are `MessageEntry` values with different flags, not separate entry types.

For run input, model visibility and persistence are independent. Every input has one of four behaviors:

| Kind | Model sees it | Store keeps it |
| ---- | ------------- | -------------- |
| Ordinary message entry | yes | yes |
| Message entry with `Meta` | yes | yes, hidden from transcript views |
| Message entry with `Ephemeral` | yes, for one run | no |
| Custom entry | no | yes |

**Meta and ephemeral are the same idea at two lifetimes.** Both are injected context: the model reads them, a transcript view hides them, because showing a reader the reminder machinery would be noise. They differ only in whether the text should still be there tomorrow.

Set `Meta` when it should. A skill body is the clearest case — reopening the session a week later, the model needs to know which instructions it was operating under, and recomputing them is not possible:

```go
body := durable.Text(skillMarkdown)
body.Meta = true
da.Run(ctx, body, durable.Text(input))
```

Use `durable.Ephemeral` when it should not. A reminder built from the current file list or today's date is true right now and misleading later; persisting it would pin stale text into the session and replay it on every resume:

```go
reminder := durable.Ephemeral(durable.Text(liveState))
da.Run(ctx, reminder, durable.Text(input))
```

**Custom entries are the mirror image**: persisted, never sent to the model. Applications define them by embedding `session.CustomEntry`, and register the type once with `session.RegisterCustom` so a store can decode it. Artifacts, UI state, and review records live here.

Entries reach the model in the order you pass them, so a reminder written before the user's message arrives before it.

## Design: two views over one log

Nothing above is a flag the reader has to remember, because the projections do the remembering. A path through the tree is read twice, for different audiences:

- `session.ModelView` produces the `[]ai.Message` for the provider. Message entries are in, meta included; custom entries are skipped.
- `session.TranscriptView` produces what a person should see. Injected context — meta and ephemeral alike — is hidden; custom entries are kept.

One log, two audiences, no second copy to keep in sync.

## Design: ephemeral entries stay off the durable chain

An ephemeral entry is recorded in the agent's in-memory log and gets an ID, so `Entries` returns it. What it never does is join the parent chain: it hangs off the current leaf without advancing it.

That is not a stylistic choice. The tree is derived entirely from parent pointers, and the walk that rebuilds a path stops when it meets a parent it cannot resolve. If a stored entry named an ephemeral parent, then on resume — where the ephemeral entry does not exist — the walk would stop there and silently drop every entry above it. A session reading `user → reminder → user → assistant` would come back holding only the last two.

Chaining past them keeps every stored parent pointer resolvable from the store alone. Being off the chain also keeps ephemeral entries out of the active path for free, which is why the transcript, later runs, and `Fork` all skip them without knowing the flag exists.

Two consequences worth knowing. `Branch` will not target an ephemeral entry, because branching onto one would re-root the durable chain on a parent the store lacks. And the persistence receipts on run events carry only durable entries — a receipt means the data survived a crash, and an ephemeral entry has nothing to attest to. Read it back from `Entries` instead.

## Related

- [Sessions](sessions.md) — identity, lineage, and the store contract
- [Transcript Tree](tree.md) — the leaf pointer, branching, forking, compaction
- [Durable Events](events.md) — persistence receipts for stored entries
- [ai.Message](../ai/messages.md) — what a message entry wraps
- [Agent History](../agent/messages.md) — the plain loop's `[]ai.Message`, and hook-based injection
