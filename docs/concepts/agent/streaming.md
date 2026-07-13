---
title: "Streaming"
summary: "Per-run event streams, event lifecycle, and event types"
read_when:
  - Consuming agent events (streaming or blocking)
  - Understanding the event lifecycle and types
---

# Streaming

`Agent.Run` returns a `Stream` — the event stream of that single run. There is no shared broker and no subscription management: one run, one stream, one consumer. The stream is an instantiation of the generic `pkg/stream` package (`stream.Stream[agent.Event, []ai.Message]`), which connects the run's producer goroutine to the caller.

## Dual consumption

- **Streaming** — range over `Stream.Events()`, an `iter.Seq2[Event, error]`, to render events as they arrive.
- **Blocking** — call `Stream.Wait()` to discard events and block until the run completes, returning the new messages it produced. `agent.Prompt(ctx, a, input)` wraps Run + Wait for the send-text-get-answer case.

Both patterns mirror `ai.EventStream` (`Events()` / `Result()`), so the agent layer streams the same way the provider layer does.

## Error semantics: one channel

The iterator's error value is the only error channel. A successful run ends with `agent_end` and nil errors throughout; a failed run yields the events produced so far, then a final iteration with a zero `Event` and the run's error. There is no `agent_end` on failure and no `Err` field on events. `Wait()` returns the same terminal error.

Cancelling the context passed to `Run` aborts the run; the stream ends with the context's error. For subprocess agents, cancellation interrupts or kills the child per that backend's semantics (the Claude agent sends a stream-json interrupt and keeps the subprocess alive; Codex/Cursor kill the per-turn child).

## Event lifecycle

Each run's stream carries one bracket pair. Session lifecycle events do not exist at this layer — backend/session lifetime is the caller's concern (see `pkg/durable` for persistent sessions). For CLI agents, `agent_start` carries the backend's `SessionID` once known.

Caller-supplied input messages are **not** echoed as `message_start` / `message_end` events — the caller already has them; they are appended to history before the run begins. Only messages produced inside the loop (assistant outputs, tool results, hook-injected follow-ups) are emitted. The `Event.Input` flag marks messages a `HookBeforeStop` injected, so consumers persisting from the event stream can distinguish follow-ups from model output.

A successful run emits events in this order:

```
agent_start               ← first event; carries SessionID if available
  turn_start
    message_start (assistant)
      message_update  ← repeated as tokens stream (provider-dependent)
    message_end
    ┌── for each tool call (interleaved per-call) ──┐
    tool_execution_start
      tool_execution_update  ← optional streaming progress
    tool_execution_end
    message_start (tool result)
    message_end
    └────────────────────────────────────────────────┘
  turn_end
  turn_start  ← next turn if tools were called
    ...
  turn_end
  message_start (follow-up, Input=true)  ← if HookBeforeStop injects messages
  message_end (Input=true)
  turn_start  ← loop continues
    ...
  turn_end
agent_end                 ← carries new Messages and accumulated Usage
```

`message_update` events are provider-dependent. The `Default` agent
emits one per provider delta as text/thinking/tool blocks accumulate.
Transports that deliver complete messages per line (e.g. the Claude CLI
agent's NDJSON `assistant` lines) skip directly from `message_start` to
`message_end` with no intermediate updates.

When tool calls run in parallel (all calls in the batch are
parallel-safe), per-call event groups remain self-consistent — each
goroutine pushes its own `tool_execution_start → tool_execution_end →
message_start (tool result) → message_end` as a contiguous sub-sequence —
but groups for different calls may interleave with each other.

If the provider stream errors mid-message, the agent still emits
`message_end` (carrying the partial accumulated message) so consumers
tracking message scope never see a dangling `message_start`. The
matching `turn_end` follows, then the stream ends with the error.

## Incremental message accumulation

During streaming, the agent maintains a partial `ai.Message` that grows as provider deltas arrive. Every `message_update` carries two views:

- **`AssistantEvent`** — the raw provider delta with `ContentIndex` and `Delta` for append-style rendering.
- **`Message`** — an independent snapshot of the accumulated message at that point.

`message_start` fires on the first non-done provider event, before any content arrives. `message_end` carries the provider's final authoritative message.

Design: providers emit bare deltas (no `Message` on delta events — only `EventDone` carries the final message). The agent's `streamTurn` bridges this by accumulating content blocks incrementally.

## Abandoning a stream

Breaking out of `Events()` early does not stop the run — the producer keeps executing with subsequent events dropped, so history stays consistent. Cancel the run's context to abort it, or call `Wait()` to block until it finishes.

## Event design: flat struct, not union types

Go doesn't have discriminated unions. Events are a single `Event` struct with a `Type` discriminator and fields populated per type. Unused fields are zero-valued. Custom `MarshalJSON` includes only relevant fields per event type for a clean wire format. `agent_start`/`agent_end` stay as explicit events (rather than relying on stream boundaries) because serialized event logs need in-band run brackets.

## Related

- [Agent](/concepts/agent/agent) — construction, options, entry points
- [Agent State](/concepts/agent/agent-state) — runtime state observability
