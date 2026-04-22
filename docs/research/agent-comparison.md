# Agent Package: Go vs TypeScript Comparison

Comparison of `pi-go/pkg/agent` (Go) against `pi-mono/packages/agent` (TypeScript reference implementation).

## Event Type Mapping

| Go (`EventType`)         | TS (`AgentEvent.type`)  | Notes |
| ------------------------ | ----------------------- | ----- |
| `agent_start`            | `agent_start`           | exact |
| `agent_end`              | `agent_end`             | exact |
| `turn_start`             | `turn_start`            | exact |
| `turn_end`               | `turn_end`              | exact |
| `message_start`          | `message_start`         | exact |
| `message_update`         | `message_update`        | exact (previously `message_delta` in Go) |
| `message_end`            | `message_end`           | exact |
| `tool_execution_start`   | `tool_execution_start`  | exact (previously `tool_start` in Go) |
| `tool_execution_update`  | `tool_execution_update` | exact (previously `tool_update` in Go) |
| `tool_execution_end`     | `tool_execution_end`    | exact (previously `tool_end` in Go) |

All event type names are now aligned between Go and TypeScript.

## Core Loop: Aligned

Both follow the same loop structure:

1. Emit `agent_start`, `turn_start`
2. Emit `message_start`/`message_end` for user messages
3. Stream assistant response, emitting `message_start` → `message_update` → `message_end`
4. If tool calls present: execute tools sequentially with `tool_execution_start` → `tool_execution_update` → `tool_execution_end`, emit tool result as `message_start`/`message_end`
5. Emit `turn_end`, start new turn, goto 3
6. If no tool calls: emit `turn_end`, `agent_end`

## Event Struct Comparison

### Go `Event` Fields
- `Type EventType` — event discriminator
- `Messages []ai.Message` — new messages (agent_end)
- `Usage ai.Usage` — accumulated token usage (agent_end)
- `Err error` — error (agent_end)
- `ToolResults []ai.Message` — tool results (turn_end)
- `Message *ai.Message` — message content (message_start/update/end, turn_end)
- `AssistantEvent *ai.Event` — underlying AI streaming event (message_update)
- `ToolCallID string` — tool call ID (tool_execution_*)
- `ToolName string` — tool name (tool_execution_*)
- `Args map[string]any` — tool arguments (tool_execution_start)
- `PartialResult any` — intermediate result (tool_execution_update)
- `Result any` — final result (tool_execution_end)
- `IsError bool` — error flag (tool_execution_end)

### TS `AgentEvent` Fields
Equivalent fields via discriminated union types. Notable differences:
- TS `agent_end` returns `messages: AgentMessage[]` (no usage or error fields on the event itself)
- TS `turn_end` has `message: AgentMessage` + `toolResults: ToolResultMessage[]`
- Go carries `Usage` on `agent_end`; TS tracks usage elsewhere

## Missing from Go

### ~~1. Steering & Follow-up Hooks~~ — Implemented

Go covers both TS hook points via the unified hook system:

- `getFollowUpMessages` → `HookBeforeStop`: when the agent would stop, the hook can inject messages to continue the loop.
- `getSteeringMessages` → `HookAfterTurn`: hooks can inspect `TurnResult.ToolResults` and append steering messages to the history mid-run.

**Still missing**: queue management methods (`steer()`, `clearSteeringQueue()`) and delivery modes (`"all"` vs `"one-at-a-time"`).

### ~~2. Context Transform Pipeline~~ — Implemented

Go's `HookBeforeCall` combines both TS stages (`transformContext` + `convertToLlm`) into one hook event. Hooks can filter at the agent message level (via `HookOutput.Messages`) or override the final LLM messages (via `HookOutput.LLMMessages`). Multiple hooks chain: each sees the previous hook's filtered messages.

Additionally, `HookAfterTurn` enables post-turn message history mutation (e.g. compaction), which TS handles via `transformContext`.

### ~~3. Extensible Message Types~~ — Implemented

Go has `CustomMessage` — applications embed it to define their own message types. Custom messages participate in the conversation history and are visible to hooks. The `HookBeforeCall` hook controls how they are converted (or filtered out) for LLM calls.

### 4. Observable State (`AgentState`)

TS maintains an `AgentState` struct with:

- `systemPrompt: string` — current system prompt
- `model: Model<any>` — current model
- `thinkingLevel: ThinkingLevel` — thinking level ("off"|"minimal"|"low"|"medium"|"high"|"xhigh")
- `tools: AgentTool<any>[]` — current tools
- `messages: AgentMessage[]` — full message history
- `isStreaming: boolean` — whether the agent is mid-run
- `streamMessage: AgentMessage | null` — current partial message during streaming
- `pendingToolCalls: Set<string>` — set of in-flight tool call IDs
- `error?: string` — last error message

Go tracks only `running bool` on the `Agent` struct.

### 5. Richer Tool Interface (`AgentTool`)

TS `AgentTool` extends `Tool` with:

- `label: string` — human-readable display name for UI
- `execute(toolCallId, params, signal?, onUpdate?)` — with `AbortSignal` for cancellation and streaming update callback
- `AgentToolResult<T>` with `content: (TextContent | ImageContent)[]` + `details: T` (arbitrary structured data for UI/logging)

Go uses `ai.Tool` with `Info() ToolInfo` + `Run(ctx, ToolCallReq) (ToolResult, error)`. Go's `ToolCallReq` has an `OnUpdate func(ToolResult)` callback for streaming, which partially covers TS's `onUpdate`. Go lacks the `label` field and the typed `details` in results.

### 6. Event Listener Pattern

TS uses push-based `subscribe(fn) → unsubscribe`. Multiple listeners can observe events concurrently.

Go uses pull-based `EventStream.Events(ctx)` iterator (`iter.Seq2[Event, error]`). Multiple subscribers supported — each `Events(ctx)` call creates an independent subscription with replay via `pubsub.Broker`.

### 7. Dynamic API Key Resolution

TS `getApiKey(provider: string) → Promise<string | undefined> | string | undefined` resolves API keys per-call, supporting expiring OAuth tokens (e.g., GitHub Copilot).

Go has no equivalent — API keys are static per model/provider.

### 8. Abort / Lifecycle Methods

TS has:
- `abort()` — signal abort via AbortController
- `waitForIdle(): Promise<void>` — wait for current run to complete
- `reset()` — clear all state (messages, queues, flags)
- `continue()` — continue from current context (for retries and processing queued messages)

Go relies on `context.Context` for cancellation. Has `Continue(ctx)` on the `Agent` interface but no wait-for-idle or reset.

### 9. Custom Stream Function

TS `streamFn` allows swapping the LLM streaming implementation (for proxy backends, testing). Also includes a `streamProxy()` utility for server-based proxy streaming with bandwidth optimization (stripping `partial` field).

Go hardcodes the stream function; no equivalent swap point.

### 10. Session ID / Transport / Retry Config

TS exposes with getter/setters:
- `sessionId` — provider session caching, changeable mid-session
- `transport` — "sse" or other supported transports
- `maxRetryDelayMs` — caps server-requested retry delays (default 60000ms)
- `thinkingBudgets` — token budgets for reasoning models

Go has none of these on the agent — some live at the `ai.Model` or `ai.Option` level.

### 11. Message Management Methods

TS provides message manipulation on the `Agent` class:
- `replaceMessages(ms)` — replace entire history
- `appendMessage(m)` — add a message
- `clearMessages()` — clear all messages

Go manages messages via `Config.History` at construction; no runtime message manipulation methods on `Default`.

### 12. Prompt Method Overloads

TS `Agent.prompt()` accepts:
- `string` with optional `ImageContent[]`
- `AgentMessage | AgentMessage[]`

Go's `Runner` interface has `Send(ctx, input string)` and `SendMessages(ctx, ...ai.Message)` — functionally equivalent but split into separate methods rather than overloads.

## Go Has, TS Doesn't

### 1. `Agent` Interface + `Factory`

Go abstracts the agent behind an `Agent` interface, with `Default` as the standard implementation. `Factory` is a function type for constructing agents. TS is a concrete class only.

### 2. Multi-Subscriber `EventStream`

Go's `EventStream` is backed by `pubsub.Broker[Event]` with blocking publish, supporting multiple concurrent subscribers with replay for late joiners. TS uses `subscribe()` callbacks — similar in spirit but push-based rather than pull-based.

### 4. Unified Hook System

Go uses a single `Hook` callback type registered per event via `WithHook(event, hook)`. Five events cover the full lifecycle: `HookBeforeCall`, `HookBeforeTool`, `HookAfterTool`, `HookAfterTurn`, `HookBeforeStop`. Multiple hooks per event run in registration order with event-specific merging semantics (chaining, first-deny-wins, last-writer-wins).

TS uses separate callback fields on `AgentLoopConfig` (`beforeToolCall`, `afterToolCall`, `transformContext`, `convertToLlm`, `getSteeringMessages`, `getFollowUpMessages`), each with a different function signature.

### 5. `MaxTurns` Guard

Go `Config.MaxTurns` prevents infinite tool-call loops. TS has no turn limit.

### 6. `ErrStream` Utility

Go provides `ErrStream(err error) *EventStream` for creating a stream that immediately emits an error `agent_end`. TS has no equivalent convenience function.

### 7. Typed `Event` with Custom JSON Serialization

Go's `Event` struct has a custom `MarshalJSON` that only includes fields relevant to each event type, keeping the wire format clean. TS uses discriminated union types which achieve similar at the type level.

### 8. Dual Consumption Patterns

Go's `EventStream` supports both streaming (`Events(ctx) iter.Seq2[Event, error]`) and blocking (`Result() ([]ai.Message, error)`) consumption, with multi-subscriber support. TS uses `subscribe()` for streaming and `await prompt()` for blocking.

## Recommendations

Features to consider porting to Go, in priority order:

1. ~~**Steering hooks**~~ — partially done via `Hooks.FollowUp`. Still missing: mid-tool-execution steering (`getSteeringMessages`), queue management, delivery modes.
2. ~~**Context transform**~~ — done via `Hooks.TransformMessages` and `Hooks.AfterTurn`.
3. **`AgentState` observability** — expose `streamMessage` and `pendingToolCalls` for UI consumers.
4. **Dynamic API key** — important for OAuth-based providers. Could be an `ai.Option` rather than agent-level.
5. ~~**Custom message types**~~ — done via `CustomMessage` embedding.
6. **Message management** — runtime methods like `ReplaceMessages`, `AppendMessage` for managing conversation history.
7. **Custom stream function** — swappable LLM streaming for proxy backends and testing.
