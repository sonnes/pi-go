---
title: "Agent State"
summary: "Runtime state observability: history via Messages, everything else via the run stream"
read_when:
  - Reading agent state during or after a run
  - Understanding state concurrency model
---

# Agent State

Configuration is frozen at construction. The agent exposes conversation history through `Messages()`. A run's progress, result, and error live on the `Stream` returned by `Run`.

## Design: state lives on the run, not the agent

Each call to `Run` returns its own stream. `Stream.Wait()` returns that run's messages and error, while `Stream.Events()` exposes its progress. State from one run cannot be mistaken for another run's result.

## Messages

`Messages()` returns a defensive copy of the full conversation history. Callers cannot corrupt internal state. `WithHistory(msgs...)` also copies its input slice.

## Concurrency

- **Reads** — `Messages()` acquires a mutex, copies, returns. Safe from any goroutine, including mid-run.
- **Writes** — only the run's producer goroutine appends to history, also under the mutex.
- **Guard** — `Run` checks a `running` flag under the lock. A concurrent `Run` fails with an "already running" stream error.

## Mid-run observability

For real-time updates during a run, consume the run's stream:

- **Streaming content** — `message_update` events carry partial assistant messages as they stream.
- **Tool progress** — `tool_execution_start`/`update`/`end` events track tool calls.
- **Completion** — `agent_end` carries all new messages and accumulated usage. An error ends the stream without this event.

## Related

- [Agent](./agent.md) — construction, options, entry points
- [Streaming](./streaming.md) — event stream and consumption patterns
