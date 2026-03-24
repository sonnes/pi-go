---
title: "Agent"
summary: "Agent interface, Default implementation, functional options, and entry points"
read_when:
  - Creating or configuring an agent
  - Understanding the agent loop lifecycle
---

# Agent

The agent manages an agentic conversation loop: prompt assembly → model inference → tool execution → event streaming, repeated until the model stops calling tools or `MaxTurns` is reached.

## Construction

`New` takes a required model and optional configuration via functional options. Configuration is frozen at construction — the agent is immutable after creation. Runtime state is tracked separately via [Agent State](/concepts/agent/agent-state).

## Design decisions

**Model is a required positional argument.** Every agent needs a model. Making it a required parameter (not an option) prevents misconfiguration and makes the constructor signature self-documenting. `New` returns `*Default`, which satisfies the `Agent` interface.

**Functional options over config struct.** Options like `WithTools`, `WithHistory`, `WithSystemPrompt`, `WithStreamOpts`, `WithMaxTurns` allow adding new parameters without breaking callers. Options are additive — pass as many as needed. `WithHistory` accepts `...Message` — both `LLMMessage` and custom messages. See [Agent Messages](/concepts/agent/messages).

**Immutable config, mutable state.** Construction parameters never change after `New`. Runtime state (messages, running status, last error) evolves during runs and is observable via `Messages()`, `IsRunning()`, and `Err()`. This separation makes it safe to read state from any goroutine without worrying about config mutations.

## Entry points

- **`Send`** — add a user text message and run the loop.
- **`SendMessages`** — add arbitrary `Message` values (LLM or custom) and run the loop.
- **`Continue`** — resume from current state without adding messages.

All return an `*EventStream`. See [Streaming](/concepts/agent/streaming).

## Agent interface

`Agent` is the interface for an agentic conversation loop, abstracting the loop for alternative implementations, testing, or decoration. The interface includes `Messages()`, `IsRunning()`, and `Err()` for state observation. `Default` is the standard implementation. `Factory` is a function type for constructing agents.

## System prompt

System prompts are built from composable, lazily-rendered `PromptSection`s. Each section has a `Key()` for deduplication and `Content(ctx)` for lazy rendering. This supports dynamic prompts that depend on runtime context (time, workspace state, etc.) without eager string concatenation at construction.

## Turn limits

`WithMaxTurns(n)` prevents infinite tool-call loops. When reached, the agent emits `agent_end` without starting another turn. Zero means unlimited.

## Cancellation

The agent respects `context.Context`. Cancelling aborts the current LLM stream and tool execution. The agent emits `agent_end` with the context error.

## Related

- [Agent Messages](/concepts/agent/messages) — extensible message type with custom message support
- [Agent State](/concepts/agent/agent-state) — runtime state observability
- [Streaming](/concepts/agent/streaming) — event stream and consumption patterns
