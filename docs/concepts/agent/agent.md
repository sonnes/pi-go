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

`New(lm ai.LanguageModel, opts ...Option)` takes a model already bound to a text provider, applies functional options, and returns `*Default`, which satisfies the `Agent` interface. Configuration is frozen at construction. Runtime state is tracked separately via [Agent State](./agent-state.md).

Build a language model directly with `ai.NewLanguageModel`, or resolve one through `catalog.Catalog.LanguageModel`. The agent calls that bound value on every turn. Passing a nil language model returns an error on the first run's stream.

## Design decisions

**The language model is the first argument.** `New` takes an `ai.LanguageModel` as its first positional argument. Tools, hooks, history, and the system prompt use functional options. `ai.GenerateText` accepts the same bound model abstraction.

**Functional options over config struct.** Options like `WithTools`, `WithHistory`, `WithSystemPrompt`, `WithStreamOpts`, `WithMaxTurns`, and `WithHook` are additive. `WithHistory` accepts `...ai.Message` and copies what it is given. See [Agent History](./messages.md).

**Extension mechanism for sub-packages.** `WithExtension(key, value)` and `WithExtensionMutator(key, mutate)` let sub-packages such as `pkg/agent/claude` carry configuration through the unified `Option` stream. Each sub-package writes to `Config.Extensions[key]` using the package name as the key, and its factory reads the same slot.

**Immutable config, mutable state.** Construction parameters never change after `New`. Runtime state (the conversation history) evolves during runs and is observable via `Messages()`. This separation makes it safe to read state from any goroutine without worrying about config mutations.

**One verb, synchronous semantics.** The interface has a single entry point — `Run(ctx, msgs...)` — returning the run's event `Stream`. The caller owns concurrency and cancellation. See [Streaming](./streaming.md) for the stream's semantics.

## Entry points

- **`Run(ctx, msgs...)`** — append messages (or none, to continue from current state) and execute the loop, returning the run's `Stream`. All errors — including pre-flight ones — surface on the stream.
- **`Stream.Events()` / `Stream.Wait()`** — consume the run event by event, or block for its new messages.
- **`Prompt(ctx, agent, input)`** — package-level convenience: send one user message, wait, return the final assistant message.

Runs are sequential: a `Run` while another run is active fails its stream with an "already running" error.

## Claude CLI subprocess agent

`pkg/agent/claude` provides an alternative `Agent` that delegates the whole loop to a long-lived `claude --print` subprocess. It starts the CLI lazily on the first `Run` with `--input-format stream-json --output-format stream-json` and stays alive across turns: each `Run` writes one `SDKUserMessage` NDJSON line to stdin and completes when the corresponding `result` line arrives.

Design:

- **Persistent subprocess.** Holding the process open amortizes startup cost across many turns and keeps session state hot inside the CLI.
- **Rich content input.** `Run` forwards the last user message's full content blocks (text + images) as an Anthropic content block array — no prompt-length ceiling and no loss of fidelity.
- **Zero-message `Run` is an error.** Stream-json mode has no "empty turn" concept. To resume a conversation, construct an agent with `WithSessionID`. Then call `Run` with the next user input. The subprocess receives `--resume` when it starts.
- **Cancellation interrupts, `Close` tears down.** Cancellation sends a stream-json interrupt. The subprocess remains available for the next `Run`. `Close` closes stdin and returns the exit error. It uses `SIGINT` or `SIGKILL` if the subprocess does not stop.
- **MCP servers via `WithMCPConfig`.** Pass an absolute `.mcp.json` path or an inline JSON document (`{"mcpServers": {...}}`). The CLI receives the value through `claude --mcp-config`. An empty string disables the flag.

## Codex CLI subprocess agent

`pkg/agent/codex` provides an `Agent` through the Codex CLI's non-interactive JSONL mode. The first `Run` starts `codex exec --json`. Later runs use `codex exec resume --json <thread-id>` after the CLI reports a thread ID.

Design:

- **Subprocess per turn.** Codex does not expose a Claude-style persistent stdin protocol, so each run starts a fresh non-interactive process. Cancelling the run's ctx kills the child.
- **Thread resume.** `SessionID()` returns the Codex thread ID captured from `thread.started`. `WithSessionID` seeds a new agent with an existing thread ID.
- **Command execution events.** Codex `command_execution` items produce `tool_execution_start` and `tool_execution_end` events with the tool name `bash`. The turn's `ToolResults` contains the command output.
- **Zero-message `Run` is an error.** Pass the next user prompt to resume the captured thread.

## Agent interface

`Agent` is the interface for an agentic conversation loop, abstracting the loop for alternative implementations, testing, or decoration. It is three methods: `Run(ctx, msgs...) *Stream` executes the loop, `Messages()` observes the history, and `Close() error` releases backend resources (a no-op for in-process agents). Small on purpose — a wrapper like `pkg/durable` covers three methods, not ten, and a new backend implements only the run itself. `Default` is the standard implementation.

`pkg/durable` mirrors the shape but not the signature: its `Run` takes `session.Entry` values, not `ai.Message` values, so it does not satisfy this interface (its `Stream` carries durable events besides). The entry is durable's currency everywhere else — `Append`, `Entries`, `Transcript`, persistence receipts — and taking it at the input boundary too is what lets one turn carry four different kinds of input: the ordinary user entry, injected context the model reads and the transcript hides, a custom entry the model never sees, and an ephemeral entry that reaches the model without ever being written to the store.

## Agent registry

`catalog.Catalog` keeps custom agent factories beside its typed providers and models. Register a factory under a kind such as `"claude"`:

```go
cat.RegisterAgent("claude", claude.Factory())
```

`cat.Agent(spec, opts...)` routes a matching kind to its custom factory. When no custom factory exists, it resolves the spec through `LanguageModel` and constructs the standard in-process agent.

Design:

- **Factory shape.** `agent.Factory` accepts the full spec and the common `agent.Option` stream, then returns an `Agent` or an error.
- **Prefix routes the kind.** `cat.Agent("claude/sonnet")` uses the registered `"claude"` factory. A spec without a custom kind follows the standard text-provider path.
- **One provider model index.** Typed provider resolution uses the catalog's shared model metadata. A custom factory receives the full spec and can interpret its model segment directly.

## System prompt

`WithSystemPrompt(string)` sets the system prompt passed to the provider on every LLM call. Callers assemble the string themselves — join sections with `\n\n`, template in dynamic context, or load from disk before constructing the agent.

## Hooks

`WithHook(event, hook)` registers lifecycle callbacks that extend the agent loop without modifying its core. All hooks share a single callback signature — `func(ctx, *HookInput) (*HookOutput, error)` — with event-specific fields on `HookInput` and `HookOutput`. Multiple hooks per event run in registration order.

Five events cover the lifecycle:

- **`HookBeforeCall`** — fires before each LLM call. Hooks can filter or replace the `[]ai.Message` sent to the model via `HookOutput.Messages`. Multiple hooks chain: each sees the previous hook's filtered messages. The full history is sent when no hook overrides.
- **`HookBeforeTool`** — fires before a tool executes. Return `HookOutput{Deny: true}` to block execution (produces an error tool result). First deny short-circuits — later hooks are skipped.
- **`HookAfterTool`** — fires after a tool executes. Return `HookOutput{ToolResult: &modified}` to override the result. Multiple hooks chain: each sees the previous hook's modified result.
- **`HookAfterTurn`** — fires after each turn completes. `HookInput.Turn` carries the assistant message, tool results, and usage. Return `HookOutput{Messages: replacement}` to replace the message history for compaction or steering.
- **`HookBeforeStop`** — fires when the agent would stop (no tool calls). Return `HookOutput{FollowUp: msgs}` to inject messages and continue the loop. Respects `MaxTurns`. First non-empty follow-up wins.

All hook events use one callback type with event-specific fields on `HookInput` and `HookOutput`.

## Turn limits

`WithMaxTurns(n)` prevents infinite tool-call loops. When reached, the agent emits `agent_end` without starting another turn. Zero means unlimited.

## Cancellation

The context passed to `Run` owns the run. Cancellation stops the current LLM stream and tool execution. The stream ends with the context error and does not emit `agent_end`. See [Streaming](./streaming.md). The agent remains available for the next `Run`.

## Related

- [Agent History](./messages.md) — how the loop holds history, and where hooks can change it
- [Agent State](./agent-state.md) — runtime state observability
- [Streaming](./streaming.md) — event stream and consumption patterns
