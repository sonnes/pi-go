// Package durable turns the [agent.Agent] loop into an agent that
// survives process restarts.
//
// It builds on the persistence primitives in pkg/session — the
// [session.Session] existence record, the [session.Entry] tree, and the
// [session.Store] contract — and adds the agent behavior on top:
//
//   - [New] binds a session ID to an inner agent loop, creating the
//     session or hydrating its history from the store.
//   - The transcript is an append-only tree with a leaf pointer marking
//     the active position. [Agent.Branch] moves the leaf to an earlier
//     entry so the next turn grows a sibling; history is never mutated
//     or deleted. Edit, retry, rewind, [Agent.Fork], and [Agent.Compact]
//     all derive from the same mechanism.
//
// A durable [Agent] retains only its session ID and transcript working
// state. Application metadata — titles, modes, model choices — lives in
// the application's own storage, keyed by the session ID; keeping it
// outside the SDK avoids stale copies and keeps application state out
// of transcript control flow.
//
// Persistence is per message: run input is persisted before the run
// starts, and every message the loop produces is persisted when it
// completes. Input is [session.Entry] values rather than messages, so
// the caller decides at the input boundary what the model and the
// transcript each see — an ordinary user entry from [Text], a custom
// entry that persists without reaching the model, or injected context
// the model reads and the transcript hides. Injected context comes in a
// durable flavor and a non-durable one: a meta entry is written and
// returns on resume, an [Ephemeral] one is shown to the model for a
// single run and never written at all.
//
// Run events ride the run's [Stream] and double as durability receipts
// — a lifted agent_start or message_end is forwarded only after its
// entries are in the store, so anything a consumer has seen complete
// survives a crash. Ephemeral entries are the one thing receipts never
// carry: there is nothing durable to attest to.
//
// Session lifecycle mutations happen outside a run, so [WithPublisher]
// sends their events through a separate [Publisher]. [New],
// [Agent.Branch], [Agent.Fork], and [Agent.Compact] publish only after
// their effect commits. Application metadata changes do not publish
// because they never pass through the agent or its store.
//
// A crash can leave an assistant tool call without its results; the
// model view repairs it on resume by synthesizing interrupted tool
// results (see [Agent.Messages]).
//
// # Middleware
//
// [Middleware] wraps a whole run — the http.Handler idiom, one level
// down. It sees the entries going in, the [Stream] coming out, or it
// refuses the run outright by returning [Fail] instead of calling
// through. [WithMiddleware] registers it, first registered outermost.
//
// The chain is built once per agent instance and runs synchronously on
// the caller's goroutine, before the producer starts — so a middleware
// that adds entries has done so by the time [Agent.Run] hands back a
// stream, and one that refuses never starts a producer at all.
//
// Because the chain is per instance, a [Middleware] closure is the
// right place for per-session state: a "once per session" notice holds
// a boolean there. [Agent.Fork] re-instantiates the chain for the
// child by re-invoking each Middleware, so the child gets its own
// closures — the parent's counters, flags, and caches stay the
// parent's.
//
// Middleware sees whole runs. Interception inside a run — per model
// call, per tool — is [agent.Hook], forwarded to the inner loop
// untouched. The two do not overlap.
//
// See docs/concepts/durable/ for the design rationale: entries.md on the
// input kinds and views, sessions.md on ownership and storage, tree.md on
// branching and compaction, and events.md on run receipts and lifecycle
// publishing.
package durable
