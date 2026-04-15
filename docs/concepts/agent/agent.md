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

**Functional options over config struct.** Options like `WithTools`, `WithHistory`, `WithSystemPrompt`, `WithStreamOpts`, `WithMaxTurns`, `WithHook` allow adding new parameters without breaking callers. Options are additive — pass as many as needed. `WithHistory` accepts `...Message` — both `LLMMessage` and custom messages. See [Agent Messages](/concepts/agent/messages).

**Immutable config, mutable state.** Construction parameters never change after `New`. Runtime state (messages, running status, last error) evolves during runs and is observable via `Messages()`, `IsRunning()`, and `Err()`. This separation makes it safe to read state from any goroutine without worrying about config mutations.

## Entry points

- **`Send`** — add a user text message and run the loop.
- **`SendMessages`** — add arbitrary `Message` values (LLM or custom) and run the loop.
- **`Continue`** — resume from current state without adding messages.
- **`Wait`** — block until the current run completes and return all new messages.

All entry points return `error` for immediate failures (e.g. already running). Events flow through `Subscribe(ctx)`. See [Streaming](/concepts/agent/streaming).

## Claude CLI subprocess agent

`pkg/agent/claude` provides an alternative `Agent` that delegates the whole loop to a long-lived `claude --print` subprocess. It starts the CLI lazily on first `Send` with `--input-format stream-json --output-format stream-json` and stays alive across turns: each `Send` writes one `SDKUserMessage` NDJSON line to stdin and blocks until the corresponding `result` line arrives.

Design:

- **Persistent subprocess.** Holding the process open amortizes startup cost across many turns and keeps session state hot inside the CLI.
- **Rich content input.** `SendMessages` forwards the last user message's full content blocks (text + images) as an Anthropic content block array — no prompt-length ceiling and no loss of fidelity.
- **`Continue` is not supported.** Stream-json mode has no "empty turn" concept. To resume a prior conversation, construct a new agent with `WithSessionID` and call `Send` with the next user input; `--resume` is passed at subprocess launch.
- **`Close` tears down the subprocess.** Closing stdin gives the CLI a chance to drain before `SIGINT`/`SIGKILL` fallback.

## Agent interface

`Agent` is the interface for an agentic conversation loop, abstracting the loop for alternative implementations, testing, or decoration. The interface embeds `pubsub.Subscriber[Event]` so consumers can subscribe to events. It includes `Wait()` for blocking completion, plus `Messages()`, `IsRunning()`, and `Err()` for state observation. `Default` is the standard implementation. `Factory` is a function type for constructing agents.

## System prompt

System prompts are built from composable `prompt.Section`s (defined in the `prompt` package). Each section has a `Key()` for identification and `Content()` for rendering. Sections are concatenated with double newlines before each LLM call.

## Hooks

`WithHook(event, hook)` registers lifecycle callbacks that extend the agent loop without modifying its core. All hooks share a single callback signature — `func(ctx, *HookInput) (*HookOutput, error)` — with event-specific fields on `HookInput` and `HookOutput`. Multiple hooks per event run in registration order.

Five events cover the lifecycle:

- **`HookBeforeCall`** — fires before each LLM call. Hooks can filter agent messages (via `HookOutput.Messages`) or override the final `[]ai.Message` sent to the model (via `HookOutput.LLMMessages`). Multiple hooks chain: each sees the previous hook's filtered messages. Falls back to `LLMMessages()` when no hook overrides. See [Agent Messages](/concepts/agent/messages).
- **`HookBeforeTool`** — fires before a tool executes. Return `HookOutput{Deny: true}` to block execution (produces an error tool result). First deny short-circuits — later hooks are skipped.
- **`HookAfterTool`** — fires after a tool executes. Return `HookOutput{ToolResult: &modified}` to override the result. Multiple hooks chain: each sees the previous hook's modified result.
- **`HookAfterTurn`** — fires after each turn completes. `HookInput.Turn` carries the assistant message, tool results, and usage. Return `HookOutput{Messages: replacement}` to replace the message history (e.g. for compaction or steering message injection).
- **`HookBeforeStop`** — fires when the agent would stop (no tool calls). Return `HookOutput{FollowUp: msgs}` to inject messages and continue the loop. Respects `MaxTurns`. First non-empty follow-up wins.

Design: a uniform callback type with event-specific input/output fields replaces the previous approach of separate function signatures per hook point. This makes the API simpler to learn (one type) while keeping event-specific semantics documented on the field types.

## Turn limits

`WithMaxTurns(n)` prevents infinite tool-call loops. When reached, the agent emits `agent_end` without starting another turn. Zero means unlimited.

## Cancellation

The agent respects `context.Context`. Cancelling aborts the current LLM stream and tool execution. The agent emits `agent_end` with the context error.

## Related

- [Agent Messages](/concepts/agent/messages) — extensible message type with custom message support
- [Agent State](/concepts/agent/agent-state) — runtime state observability
- [Streaming](/concepts/agent/streaming) — event stream and consumption patterns
- [prompt package](/concepts/prompt) — composable system prompt sections
