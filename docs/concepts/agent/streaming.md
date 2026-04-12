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

`agent_start` signals that the backend is initialized and ready. It fires once per `Send` or `Continue` call. Input messages are always emitted before `agent_start`. If the backend fails before initialization (e.g. subprocess crash), only `agent_end` (with `Err`) is emitted — subscribers should not assume `agent_start` always precedes `agent_end`. For the Claude CLI agent, `agent_start` carries a `SessionID` for session resumption.

A complete run emits events in this order:

```
message_start (user)      ← input messages, before agent starts
message_end
agent_start               ← backend ready; carries SessionID if available
  turn_start
    message_start (assistant)
      message_update  ← repeated as tokens stream
    message_end
    tool_execution_start  ← if tool calls present
      tool_execution_update  ← optional streaming progress
    tool_execution_end
    message_start (tool result)
    message_end
  turn_end
  turn_start  ← next turn if tools were called
    ...
  turn_end
  message_start (follow-up)  ← if HookBeforeStop injects messages
  message_end
  turn_start  ← loop continues
    ...
  turn_end
agent_end
```

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
