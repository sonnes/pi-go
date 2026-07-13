---
title: "Agent State"
summary: "Runtime state observability: history via Messages, everything else via the run stream"
read_when:
  - Reading agent state during or after a run
  - Understanding state concurrency model
---

# Agent State

Configuration is frozen at construction; the only runtime state the interface exposes is the conversation history, via `Messages()`. Everything that used to be a getter is now a property of the run: whether a run is active, how it ended, and what it produced all live on the `Stream` returned by `Run`.

## Design: state lives on the run, not the agent

An earlier design exposed `IsRunning()` and `Err()` getters plus a `Wait()` method. All three existed only because runs were asynchronous — and `Wait`/`Err` had a footgun: called between runs, they reported the *previous* run's result. With `Run` returning a per-run stream, each run's outcome is unambiguous: `Stream.Wait()` returns that run's messages and error, and "is it running" is simply "has my stream ended". One error channel replaces four.

## Messages

`Messages()` returns a defensive copy of the full conversation history — callers cannot corrupt internal state. `WithHistory(msgs...)` copies messages in at construction; the caller's slice is not aliased.

## Concurrency

- **Reads** — `Messages()` acquires a mutex, copies, returns. Safe from any goroutine, including mid-run.
- **Writes** — only the run's producer goroutine appends to history, also under the mutex.
- **Guard** — `Run` checks a `running` flag under the lock; a concurrent `Run` fails its stream with an "already running" error.

## Mid-run observability

For real-time updates during a run, consume the run's stream:

- **Streaming content** — `message_update` events carry partial assistant messages as they stream.
- **Tool progress** — `tool_execution_start`/`update`/`end` events track tool calls.
- **Completion** — `agent_end` carries all new messages and accumulated usage; failures end the stream with an error.

## Related

- [Agent](/concepts/agent/agent) — construction, options, entry points
- [Streaming](/concepts/agent/streaming) — event stream and consumption patterns
