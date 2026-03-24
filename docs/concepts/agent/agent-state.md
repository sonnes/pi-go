---
title: "Agent State"
summary: "Runtime state observability via mutex-guarded getters"
read_when:
  - Reading agent state during or after a run
  - Understanding state concurrency model
---

# Agent State

The agent exposes runtime state through direct getters on the `Agent` interface. Configuration is frozen at construction; runtime state evolves during the loop.

## Design: mutex-guarded fields

The `Default` agent holds `running`, `messages`, and `err` as fields protected by a `sync.Mutex`. Getters acquire the lock, copy, and return. During a run, only the producer goroutine mutates these fields, and `running=true` prevents concurrent runs — so lock contention is minimal.

**Why not atomic snapshots?** An earlier design used `atomic.Pointer[State]` with copy-on-write snapshots. This created ~8 heap allocations per turn for immutable state objects that consumers rarely polled. Since events already deliver all mid-run information (streaming content, tool progress), the snapshot overhead wasn't justified.

**Why not channels/push?** State is pull-based. Callers decide when to read. Events handle the push-based case — subscribe to the `EventStream` for real-time updates.

## Getters

| Method        | Description                                  |
| ------------- | -------------------------------------------- |
| `IsRunning()` | Whether the agent loop is currently executing |
| `Err()`       | Last error from the agent loop, or nil       |
| `Messages()`  | Full conversation history including custom messages (defensive copy) |

`Messages()` returns a copy — callers cannot corrupt internal state.

## Initial state

`WithHistory(msgs...)` copies messages into the agent at construction. The caller's slice is not aliased. Both `LLMMessage` and custom messages are preserved. Initial state: not running, no error, messages from history (or empty).

## Concurrency

- **Reads** — acquire mutex, copy, return. Safe from any goroutine.
- **Writes** — only the loop's producer goroutine appends to `messages` and sets `err`. The mutex serializes reads against writes.
- **Guard** — `run()` checks `running` under the lock and rejects concurrent runs with `ErrStream`.

## Mid-run observability

For real-time updates during a run, use the `EventStream` rather than polling getters:

- **Streaming content** — `message_update` events carry partial assistant messages as they stream.
- **Tool progress** — `tool_execution_start`/`update`/`end` events track tool calls.
- **Completion** — `agent_end` carries all new messages and accumulated usage.

## Related

- [Agent](/concepts/agent/agent) — construction, options, entry points
- [Agent Messages](/concepts/agent/messages) — extensible message type with custom message support
- [Streaming](/concepts/agent/streaming) — event stream and consumption patterns
