---
title: "Agent State"
summary: "Runtime state observability via atomic snapshots"
read_when:
  - Reading agent state during or after a run
  - Understanding state concurrency model
---

# Agent State

The agent exposes runtime state as read-only snapshots via `State()`. State evolves during the loop; configuration is frozen at construction.

## Design: atomic copy-on-write snapshots

The agent holds an `atomic.Pointer[State]`. On each state change (streaming starts, tool call begins, message appended), the loop copies the current state, mutates the copy, and swaps it in via CAS. Readers always get a consistent snapshot — never a half-updated mix of fields.

**Why not locks?** Reads are lock-free (`atomic.Pointer.Load`). Any goroutine can call `State()` without blocking the agent loop. This matters for UI consumers that poll on repaint.

**Why not channels/push?** State is pull-based. Callers decide when to read. If push-based observation is needed later, an `OnStateChange func(State)` callback can be added as an option without changing the core design.

## State fields

| Getter               | Description                                       |
| -------------------- | ------------------------------------------------- |
| `IsStreaming()`      | Whether the agent loop is currently executing      |
| `StreamMessage()`    | Partial assistant message during streaming, or nil |
| `PendingToolCalls()` | Set of in-flight tool call IDs (defensive copy)    |
| `Err()`              | Last error from the agent loop, or nil             |
| `Messages()`         | Full conversation history including custom messages (defensive copy) |
| `LLMMessages()`      | LLM-only messages, filtering out custom messages   |

`Messages()`, `LLMMessages()`, and `PendingToolCalls()` return copies — callers cannot corrupt internal state.

## Initial state

`WithHistory(msgs...)` copies messages into the first snapshot. The caller's slice is not aliased. `msgs` are `Message` values — both `LLMMessage` and custom messages are preserved. Initial state: not streaming, no stream message, no pending tool calls, no error.

## No convenience wrappers

`Agent` exposes a single `State()` method. There are no `Messages()` or `IsRunning()` shortcuts on the interface — callers read what they need from the snapshot. This keeps the interface small and avoids redundant methods that just delegate.

## Concurrency

- **Reads** — lock-free, any goroutine.
- **Writes** — CAS in the loop goroutine only. Only one loop runs at a time, so contention is minimal.

## Related

- [Agent](/concepts/agent/agent) — construction, options, entry points
- [Agent Messages](/concepts/agent/messages) — extensible message type with custom message support
- [Streaming](/concepts/agent/streaming) — event stream and consumption patterns
