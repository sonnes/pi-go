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

`New(model ai.Model, opts ...Option)` takes the model as a required argument, applies functional options, and returns `*Default`, which satisfies the `Agent` interface. Configuration is frozen at construction — the agent is immutable after creation. Runtime state is tracked separately via [Agent State](/concepts/agent/agent-state).

`Default` resolves the provider from the global `ai` registry by `Model.Provider` at call time. `WithProvider(ai.Provider)` binds a provider instance directly and skips the global lookup — useful when callers want per-agent provider wiring without mutating the process-wide registry. CLI subprocess agents (e.g. the Claude agent) ignore most model metadata and use only `Model.Name` (falling back to `Model.ID`) as the model name they pass to their CLI. A zero model with no bound provider still errors at the first `Send`.

## Design decisions

**Model is required; everything else is an option.** `New` takes the model as its first positional argument and the rest — tools, hooks, system prompt — as functional options. Making the model required moves missing-model errors to compile time and matches `ai.GenerateText`, which also takes the model positionally. The uniform constructor shape is captured by `CreateFunc` (`func(model ai.Model, opts ...Option) Agent`), which every implementation satisfies.

**Functional options over config struct.** Options like `WithTools`, `WithHistory`, `WithSystemPrompt`, `WithStreamOpts`, `WithMaxTurns`, `WithHook` allow adding new parameters without breaking callers. Options are additive — pass as many as needed. `WithHistory` accepts `...Message` — both `LLMMessage` and custom messages. See [Agent Messages](/concepts/agent/messages).

**Extension mechanism for sub-packages.** `WithExtension(key, value)` and `WithExtensionMutator(key, mutate)` let sub-packages (e.g. `pkg/agent/claude`) carry their own configuration through the unified `Option` stream. Each sub-package writes to `Config.Extensions[key]` using the package name as the key, and its create func reads the same slot. This is how a single call like `f(ai.Model{Name: "sonnet"}, claude.WithCLIPath("/x"))` composes the model, agent-level options, and sub-package options without collisions.

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
- **MCP servers via `WithMCPConfig`.** Pass either an absolute path to an `.mcp.json` file or an inline JSON document (`{"mcpServers": {...}}`); the value is forwarded verbatim to `claude --mcp-config` so MCP-provided tools become invocable inside the subprocess. Empty string disables the flag.

## Codex CLI subprocess agent

`pkg/agent/codex` provides an `Agent` backed by the Codex CLI's non-interactive JSONL mode. The first `Send` runs `codex exec --json`; when the CLI reports a thread ID, later sends run `codex exec resume --json <thread-id>` so Codex owns the conversation context.

Design:

- **Subprocess per turn.** Codex does not expose a Claude-style persistent stdin protocol, so each send starts a fresh non-interactive process.
- **Thread resume.** `SessionID()` returns the Codex thread ID captured from `thread.started`. `WithSessionID` seeds a new agent with an existing thread ID.
- **Command execution events.** Codex `command_execution` items are surfaced as `tool_execution_start` / `tool_execution_end` events with tool name `bash`; command output is attached to the turn's `ToolResults`.
- **`Continue` is not supported.** Use `Send` with the next prompt to resume the captured thread.

## Agent interface

`Agent` is the interface for an agentic conversation loop, abstracting the loop for alternative implementations, testing, or decoration. The interface embeds `pubsub.Subscriber[Event]` so consumers can subscribe to events. It includes `Wait()` for blocking completion, plus `Messages()`, `IsRunning()`, and `Err()` for state observation. `Default` is the standard implementation.

## Agent registry

`pkg/agent` keeps a string-keyed registry of agent constructors so callers can create an agent by name without importing every implementation. `Create` is the front door: it takes a `"<provider>/<model>"` spec, routes on the provider prefix to a registered constructor, resolves the model, and returns the agent. This is how the CLI selects an agent from a single `--model` flag (e.g. `pi --model claude/sonnet`).

Design:

- **Register `New` directly.** `RegisterAgent` is generic over the concrete return type, so a package's constructor registers with no adapter or exported factory var: `agent.RegisterAgent("claude", claude.New)`. Lookups go through `GetAgent`/`Agents`; the stored, type-erased shape is `CreateFunc`. No `init()` side effects — `pkg/agent` stays decoupled from concrete implementations.
- **Prefix routes the kind.** `Create("claude/sonnet")` routes to the `claude` constructor (using `sonnet` as the model name); any unregistered prefix — e.g. `Create("anthropic-messages/claude-sonnet-4-6")` — falls back to the `Default` agent and resolves the model spec through the `ai` registry.
- **Convention: registry name == extension key == sub-package name.** The same string keys the agent registry and the `Config.Extensions` map, so collisions are avoidable by construction and create funcs are easy to find.
- **Agent-managed models.** `RegisterModel`/`ResolveModel`/`Models` hold models that live outside the `ai` registry — the CLI kinds, keyed `"<kind>/<id>"` (e.g. `"claude/sonnet"`) — mirroring the `ai` model registry.

## System prompt

`WithSystemPrompt(string)` sets the system prompt passed to the provider on every LLM call. Callers assemble the string themselves — join sections with `\n\n`, template in dynamic context, or load from disk before constructing the agent.

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
