// Package durable turns the [agent.Agent] loop into an agent that
// survives process restarts.
//
// It builds on the persistence primitives in pkg/session — the
// [session.Session] identity, the [session.Entry] tree, and the
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
// Persistence is per message: run input is persisted before the run
// starts, and every message the loop produces is persisted when it
// completes. Run events ride the run's [Stream] and double as
// durability receipts — a lifted agent_start or message_end is
// forwarded only after its entries are in the store, so anything a
// consumer has seen complete survives a crash.
//
// Session events (session_init, session_updated, session_branched,
// session_forked, session_compacted) are delivered to the [Publisher]
// injected via [WithPublisher] at the moment their mutation commits.
// The application owns delivery — forward, fan out, or drop; without a
// publisher they are discarded. There is no broker.
//
// A crash can leave an assistant tool call without its results; the
// model view repairs it on resume by synthesizing interrupted tool
// results (see [Agent.Messages]).
//
// See docs/plans/durable-agents.md for the implementation plan and
// docs/launch/durable-agents.html for the design overview.
package durable
