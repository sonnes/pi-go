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
  message_start (follow-up)  ← if HookBeforeStop injects messages
  message_end
  turn_start  ← loop continues
    ...
  turn_end
agent_end
```

## Event design: flat struct, not union types

Go doesn't have discriminated unions. Events are a single `Event` struct with a `Type` discriminator and fields populated per type. Unused fields are zero-valued. Custom `MarshalJSON` includes only relevant fields per event type for a clean wire format.

## Related

- [Agent](/concepts/agent/agent) — construction, options, entry points
- [Agent State](/concepts/agent/agent-state) — runtime state observability
