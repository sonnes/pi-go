---
title: "Streaming"
summary: "Agent event subscription, event lifecycle, and event types"
read_when:
  - Consuming agent events (streaming or blocking)
  - Understanding the event lifecycle and types
---

# Streaming

The agent embeds `pubsub.Subscriber[Event]`, exposing a single event stream per agent. All entry points (`Send`, `SendMessages`, `Continue`) publish events to the agent's broker. Consumers subscribe once and receive events across all calls.

## Dual consumption

- **Streaming** — subscribe to events via `Subscribe(ctx)`, iterate the channel.
- **Blocking** — call `Wait(ctx)` to block until the current run completes and get all new messages.

## Multi-subscriber

The agent's broker uses blocking publish, so events are never dropped. Multiple goroutines can call `Subscribe(ctx)` concurrently — each gets an independent subscription. Late subscribers replay buffered events via the broker's ring buffer (default 1000 events) using `pubsub.After(seq)`. Cancel the context to unsubscribe.

## Event lifecycle

The stream models two nested lifecycles:

- **Session** — the lifetime of the agent backend (subprocess for the Claude CLI agent, in-process loop for the `Default` agent). `session_init` fires once when the backend is initialized; `session_end` fires once when the agent is `Close`d. For the Claude CLI agent, `session_init` carries the subprocess `SessionID` for resumption.
- **Run** — a single `Send` / `Continue` / `SendMessages` call. `agent_start` brackets the start of every run; `agent_end` brackets the end. Many runs share one session.

`session_init` always precedes the very first `agent_start`. If the backend fails before initialization (e.g. subprocess startup error), neither `session_init` nor `agent_start` fires — only `agent_end` with `Err` set.

Caller-supplied input messages (passed to `Send` / `SendMessages`) are **not** echoed as `message_start` / `message_end` events — the caller already has those messages and they are appended to history before the run begins. Only messages produced inside the loop (assistant outputs, tool results, hook-injected follow-ups) are emitted on the stream. The `Event.Input` flag is set on `message_start` / `message_end` events for messages a `HookBeforeStop` injected, so consumers persisting from the event stream can distinguish injected follow-ups from model output.

A complete agent lifetime emits events in this order:

```
session_init              ← once, first event ever; carries SessionID if available
  agent_start             ← per Send / Continue
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
  agent_end
  agent_start             ← next Send (no second session_init)
    ...
  agent_end
session_end               ← once, on Close
```

`message_update` events are provider-dependent. The `Default` agent
emits one per provider delta as text/thinking/tool blocks accumulate.
Transports that deliver complete messages per line (e.g. the Claude CLI
agent's NDJSON `assistant` lines) skip directly from `message_start` to
`message_end` with no intermediate updates.

When tool calls run in parallel (all calls in the batch are
parallel-safe), per-call event groups remain self-consistent — each
goroutine pushes its own `tool_execution_start → tool_execution_end →
message_start (tool result) → message_end` as a contiguous sub-sequence
through the broker — but groups for different calls may interleave with
each other.

If the provider stream errors mid-message, the agent still emits
`message_end` (carrying the partial accumulated message) so consumers
tracking message scope never see a dangling `message_start`. The
matching `turn_end` and `agent_end (Err)` follow.

## Incremental message accumulation

During streaming, the agent maintains a partial `ai.Message` that grows as provider deltas arrive. Every `message_update` carries two views:

- **`AssistantEvent`** — the raw provider delta with `ContentIndex` and `Delta` for append-style rendering.
- **`Message`** — an independent snapshot of the accumulated message at that point.

`message_start` fires on the first non-done provider event, before any content arrives. `message_end` carries the provider's final authoritative message.

Design: providers emit bare deltas (no `Message` on delta events — only `EventDone` carries the final message). The agent's `streamTurn` bridges this by accumulating content blocks incrementally.

## Event design: flat struct, not union types

Go doesn't have discriminated unions. Events are a single `Event` struct with a `Type` discriminator and fields populated per type. Unused fields are zero-valued. Custom `MarshalJSON` includes only relevant fields per event type for a clean wire format.

## Related

- [Agent](/concepts/agent/agent) — construction, options, entry points
- [Agent State](/concepts/agent/agent-state) — runtime state observability
