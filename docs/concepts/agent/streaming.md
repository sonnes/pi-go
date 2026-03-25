---
title: "Streaming"
summary: "EventStream consumption patterns, event lifecycle, and event types"
read_when:
  - Consuming agent events (streaming or blocking)
  - Understanding the event lifecycle and types
---

# Streaming

Every agent entry point (`Send`, `SendMessages`, `Continue`) returns an `*EventStream`. The stream carries lifecycle events as the agent thinks, calls tools, and streams output.

## Dual consumption

`EventStream` supports two modes:

- **Streaming** — iterate events as they arrive via `Events(ctx)` (`iter.Seq2[Event, error]`).
- **Blocking** — wait for completion and get all new messages via `Result()`.

## Multi-subscriber

`EventStream` is backed by a `pubsub.Broker[Event]` with blocking publish. Multiple goroutines can call `Events(ctx)` concurrently — each gets an independent subscription. Late subscribers replay buffered events via the broker's ring buffer before switching to live events. Cancel the context to unsubscribe early.

## Event lifecycle

A complete run emits events in this order:

```
agent_start
  message_start (user)      ← input messages emitted before first turn
  message_end
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
  message_start (follow-up)  ← if FollowUp hook injects messages
  message_end
  turn_start  ← loop continues
    ...
  turn_end
agent_end
```

## Event design: flat struct, not union types

Go doesn't have discriminated unions. Events are a single `Event` struct with a `Type` discriminator and fields populated per type. Unused fields are zero-valued. Custom `MarshalJSON` includes only relevant fields per event type for a clean wire format.

## Stream utilities

- **`NewStream`** — create a stream with a producer goroutine.
- **`ErrStream`** — return a stream that immediately emits an error `agent_end`. Useful for early-exit error paths that still need to return a stream.

## Related

- [Agent](/concepts/agent/agent) — construction, options, entry points
- [Agent State](/concepts/agent/agent-state) — runtime state observability
