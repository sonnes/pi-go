// Package durable turns the [agent.Agent] loop into an agent that
// survives process restarts.
//
// The package builds on the persistence primitives in pkg/session: the
// [session.Session] existence record, the [session.Entry] tree, and the
// [session.Store] contract. It adds the agent behavior on top of them:
//
//   - [New] binds a session ID to an inner agent loop. New creates the
//     session, or it loads the history of the session from the store.
//   - The transcript is an append-only tree. A leaf pointer marks the
//     active position. [Agent.Branch] moves the leaf to an earlier
//     entry, so the next turn grows a sibling. The package never
//     changes or removes history. Edit, retry, rewind, [Agent.Fork],
//     and [Agent.Compact] all come from this one mechanism.
//
// A durable [Agent] keeps only its session ID and transcript working
// state. Application metadata lives in the storage of the application,
// keyed by the session ID. This metadata covers titles, modes, and
// model choices. Metadata outside the SDK cannot go stale, and
// application state stays out of transcript control flow.
//
// Persistence is per message. The package persists run input before the
// run starts. It persists every message the loop produces when that
// message completes.
//
// Input is [session.Entry] values rather than messages. The caller
// therefore decides at the input boundary what the model and the
// transcript each see. The choices are an ordinary user entry from
// [Text], a custom entry that persists without reaching the model, or
// injected context that the model reads and the transcript hides.
//
// Injected context comes in a durable form and a non-durable form. The
// package writes a meta entry, and that entry returns on resume. An
// [Ephemeral] entry goes to the model for a single run, and the package
// never writes it.
//
// Run events ride the [Stream] of the run. They also work as durability
// receipts. The package forwards a lifted agent_start or message_end
// only after its entries are in the store. Anything a consumer sees as
// complete therefore survives a crash. Receipts never carry ephemeral
// entries, because there is nothing durable to attest to.
//
// Session lifecycle mutations happen outside a run. [WithPublisher]
// therefore sends their events through a separate [Publisher]. [New],
// [Agent.Branch], [Agent.Fork], and [Agent.Compact] publish only after
// their effect commits. Changes to application metadata do not publish,
// because they never pass through the agent or its store.
//
// A crash can leave an assistant tool call without its results. On
// resume, the model view repairs the call. It synthesizes interrupted
// tool results (see [Agent.Messages]).
//
// # Middleware
//
// [Middleware] wraps a whole run. It is the http.Handler idiom, one
// level down. A middleware sees the entries that go in and the [Stream]
// that comes out. It can also refuse the run: it returns [Fail] instead
// of a call to the next runner. [WithMiddleware] registers a
// middleware, and the first registered is the outermost.
//
// The package builds the chain once per agent instance. The chain runs
// synchronously on the goroutine of the caller, before the producer
// starts. A middleware that adds entries therefore adds them before
// [Agent.Run] returns a stream. A middleware that refuses the run never
// starts a producer.
//
// The chain is per instance, so a [Middleware] closure is the right
// place for per-session state. A "once per session" notice holds a
// boolean there. [Agent.Fork] builds the chain again for the child and
// calls each Middleware again. The child therefore gets its own
// closures. The counters, flags, and caches of the parent stay with the
// parent.
//
// Middleware sees whole runs. Interception inside a run, per model call
// or per tool, is [agent.Hook]. The durable agent forwards a hook to
// the inner loop untouched. The two mechanisms do not overlap.
//
// The design rationale is in docs/concepts/durable/: entries.md on the
// input kinds and views, sessions.md on ownership and storage, tree.md
// on branching and compaction, and events.md on run receipts and
// lifecycle publishing.
package durable
