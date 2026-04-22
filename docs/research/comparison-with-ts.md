# Comparison with pi-mono (TypeScript)

This document tracks the API surface differences between the Go SDK (`pi-go`) and the TypeScript SDK (`pi-mono/packages/ai` + `pi-mono/packages/agent`).

## Core Abstractions — Aligned

### AI Layer (`pi-go/pkg/ai` vs `pi-mono/packages/ai`)


| Concept            | TypeScript                                                                                               | Go                                                                                                |
| ------------------ | -------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Message roles      | `"user"`, `"assistant"`, `"toolResult"`                                                                  | `RoleUser`, `RoleAssistant`, `RoleToolResult`                                                     |
| Content blocks     | `TextContent`, `ThinkingContent`, `ImageContent`, `ToolCall`                                             | `Text`, `Thinking`, `Image`, `ToolCall`                                                           |
| Streaming events   | `text_start/delta/end`, `thinking_start/delta/end`, `toolcall_start/delta/end`, `start`, `done`, `error` | Same event types                                                                                  |
| EventStream        | Async iterable + `.result()` promise                                                                     | `Events() iter.Seq2` + `Result() (*Message, error)`                                               |
| Provider interface | `ApiProvider { api, stream, streamSimple }`                                                              | `Provider { API(), StreamText() }`                                                                |
| Provider registry  | `registerApiProvider()` / `getApiProvider()` / `clearApiProviders()`                                     | `RegisterProvider()` / `GetProvider()` / `ClearProviders()`                                       |
| Tool definition    | `Tool<TParameters>` with TypeBox schema                                                                  | `ToolDef[In, Out]` with jsonschema                                                                |
| Model struct       | `Model<TApi>` with id, name, api, provider, costs, etc.                                                  | `Model` struct with same fields                                                                   |
| Prompt / Context   | `Context { systemPrompt, messages, tools }`                                                              | `Prompt { System, Messages, Tools }`                                                              |
| Stop reasons       | `"stop"`, `"length"`, `"toolUse"`, `"error"`, `"aborted"`                                                | `StopReasonStop`, `StopReasonLength`, `StopReasonToolUse`, `StopReasonError`, `StopReasonAborted` |
| ThinkingLevel      | `"minimal"` / `"low"` / `"medium"` / `"high"` / `"xhigh"`                                                | Same values                                                                                       |
| ToolChoice         | auto / none / required + specific tool                                                                   | Same                                                                                              |
| Usage tracking     | input, output, cacheRead, cacheWrite, totalTokens, cost                                                  | Same fields                                                                                       |
| Providers          | Anthropic, OpenAI, Google (+ many more)                                                                  | Anthropic, OpenAI, Google                                                                         |


### Agent Layer (`pi-go/pkg/agent` vs `pi-mono/packages/agent`)


| Concept                  | TypeScript                                                                                                                                                                                                      | Go                                                                                    |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Agent loop               | `runAgentLoop()` / `runAgentLoopContinue()`                                                                                                                                                                     | `Default.loop()` via `Send`/`Continue`                                                |
| Entry points             | `agent.prompt(input)` / `agent.continue()`                                                                                                                                                                      | `agent.Send(ctx, input)` / `agent.SendMessages(ctx, msgs...)` / `agent.Continue(ctx)` |
| Event types              | `AgentEvent` discriminated union: `agent_start`, `agent_end`, `turn_start`, `turn_end`, `message_start`, `message_update`, `message_end`, `tool_execution_start`, `tool_execution_update`, `tool_execution_end` | `Event` struct with `EventType` discriminator — same event set                        |
| Event delivery           | `agent.subscribe(fn)` callback + `agent.waitForIdle()`                                                                                                                                                          | `EventStream` with `Events()` iterator + `Result()` blocking                          |
| Custom messages          | `AgentMessage = Message | CustomAgentMessages[keyof CustomAgentMessages]` via declaration merging                                                                                                               | `Message` sealed interface with `LLMMessage` + `CustomMessage` embedding              |
| Message filtering        | `convertToLlm` callback filters custom messages before LLM calls                                                                                                                                                | `LLMMessages()` filters `CustomMessage` values before LLM calls                       |
| Tool execution modes     | `"sequential"` / `"parallel"` (config-level)                                                                                                                                                                    | Per-tool `Parallel` flag on `tool.Info()`                                             |
| Tool error recovery      | Error tool results sent back to model (non-fatal)                                                                                                                                                               | Same — `ErrorToolResultMessage` sent back to model                                    |
| Panic/exception recovery | N/A (JS doesn't have panics)                                                                                                                                                                                    | Per-tool `recover()` converts panics to error results                                 |
| System prompt            | `agent.setSystemPrompt(string)` — mutable, plain string                                                                                                                                                         | `WithSystemPrompt(string)` — immutable, set once at construction                      |
| Concurrent run guard     | `isStreaming` flag, throws on double-prompt                                                                                                                                                                     | `running` flag under mutex, returns `ErrStream`                                       |
| Cancellation             | `AbortController` / `AbortSignal`                                                                                                                                                                               | `context.Context` cancellation                                                        |


## API Design Differences

### AI Layer


| Area                   | TypeScript                                                       | Go                                                    | Rationale                                   |
| ---------------------- | ---------------------------------------------------------------- | ----------------------------------------------------- | ------------------------------------------- |
| Naming                 | `Context` for prompt container                                   | `Prompt`                                              | Avoids collision with `context.Context`     |
| Options                | Plain objects with spreads                                       | Functional options (`WithTemperature()`, etc.)        | Idiomatic Go pattern                        |
| Error handling         | try/catch + error events                                         | `error` returns + `EventError`                        | Go error conventions                        |
| Generics               | TypeBox schemas at runtime                                       | `GenerateObject[T]`, `ToolDef[In, Out]`               | Go generics enable compile-time type safety |
| Provider dispatch      | Per-provider stream functions + registry                         | Interface-based dispatch via registry only            | Go interface polymorphism                   |
| stream vs streamSimple | `stream()` (raw options) vs `streamSimple()` (unified reasoning) | Single `StreamText()` with `ThinkingLevel` in options | Go combines both paths — simpler surface    |
| Abort / cancellation   | `AbortSignal` on options                                         | `context.Context` cancellation                        | Native Go cancellation                      |


### Agent Layer


| Area                      | TypeScript                                                                                                                           | Go                                                                                   | Rationale                                                             |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | --------------------------------------------------------------------- |
| State exposure            | `agent.state` property returns full `AgentState` object with `isStreaming`, `streamMessage`, `pendingToolCalls`, `messages`, `error` | `Messages()`, `IsRunning()`, `Err()` getters with mutex                              | Go: events deliver mid-run info; polling snapshot was wasteful        |
| State mutability          | Mutable: `setModel()`, `setTools()`, `setSystemPrompt()`, `replaceMessages()`, `setThinkingLevel()`                                  | Immutable after `New()`: config frozen at construction                               | Go: immutability simplifies concurrency                               |
| Event subscription        | Push-based: `agent.subscribe(fn)` returns unsubscribe function                                                                       | Pull-based: `EventStream.Events()` iterator per call                                 | Go: iterator pattern is idiomatic, multi-subscriber via pubsub broker |
| Stream function           | Configurable `streamFn` for proxy backends                                                                                           | Fixed to `ai.StreamText` via provider registry                                       | Go: provider interface handles this                                   |
| Tool execution control    | Config-level `"sequential"` / `"parallel"` mode for all tools                                                                        | Per-tool `Parallel` flag — batch runs parallel only if all tools in batch are marked | Go: finer-grained control per tool                                    |
| Custom message conversion | `convertToLlm` callback — user-defined transform                                                                                     | `LLMMessages()` built-in filter — sealed `Message` interface handles it              | Go: sealed interface makes filtering deterministic                    |
| Return type               | `async prompt()` returns `Promise<void>`, messages accumulate on agent                                                               | `Send()` returns `*EventStream` with `Result() ([]ai.Message, error)`                | Go: streams are values, not side effects                              |


## Missing from Go

### Providers


| Provider               | TS API identifier         | Status          |
| ---------------------- | ------------------------- | --------------- |
| OpenAI Responses       | `openai-responses`        | Not implemented |
| Azure OpenAI Responses | `azure-openai-responses`  | Not implemented |
| OpenAI Codex Responses | `openai-codex-responses`  | Not implemented |
| Mistral                | `mistral-conversations`   | Not implemented |
| AWS Bedrock            | `bedrock-converse-stream` | Not implemented |
| Google Vertex          | `google-vertex`           | Not implemented |
| Google Gemini CLI      | `google-gemini-cli`       | Not implemented |


### AI Features


| Feature                | Description                                                                                                                                       |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Model registry         | TS has `getModel()`, `getModels()`, `getProviders()` with a generated model database. Go requires users to construct `Model` structs manually.    |
| OAuth                  | TS has full OAuth with PKCE for Anthropic, GitHub Copilot, Google CLI, etc.                                                                       |
| `CacheRetention`       | `"none"` / `"short"` / `"long"` option for prompt caching hints.                                                                                  |
| `Transport`            | `"sse"` / `"websocket"` / `"auto"` transport selection.                                                                                           |
| `transformMessages()`  | Cross-provider message normalization: drops incompatible thinking blocks, fixes tool call IDs, inserts synthetic tool results for orphaned calls. |
| `isContextOverflow()`  | Context overflow detection with regex patterns.                                                                                                   |
| `parseStreamingJson()` | Partial JSON parsing for incomplete tool call arguments during streaming.                                                                         |
| `validateToolCall()`   | Runtime AJV validation of tool arguments against schema.                                                                                          |
| `ThinkingBudgets`      | Custom token allocations per thinking level. Go hardcodes budgets per provider.                                                                   |
| `onPayload`            | Raw payload inspection callback on stream options.                                                                                                |
| `sessionId`            | Session tracking option.                                                                                                                          |
| Provider routing       | OpenRouter / Vercel AI Gateway routing configuration.                                                                                             |
| `sanitizeSurrogates()` | Unicode surrogate cleaning utility.                                                                                                               |
| `getEnvApiKey()`       | Environment variable resolution for API keys by provider name.                                                                                    |


### Agent Features


| Feature                 | Description                                                                                                                         |
| ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Steering messages       | `agent.steer(msg)` queues messages delivered between turns while the agent is running. Supports `"all"` or `"one-at-a-time"` modes. |
| Follow-up messages      | `agent.followUp(msg)` queues messages delivered after the agent finishes. Continues the loop with the queued message.               |
| Context transforms      | `transformContext` callback for pruning, injecting external context before LLM calls.                                               |
| Before/after tool hooks | `beforeToolCall` can block execution; `afterToolCall` can override results.                                                         |
| Dynamic API keys        | `getApiKey` callback resolves keys per-call for expiring OAuth tokens.                                                              |
| Mutable config          | Model, tools, system prompt, thinking level changeable between runs.                                                                |
| `maxRetryDelayMs`       | Cap on server-requested retry delays.                                                                                               |
| `agent.abort()`         | Explicit abort via `AbortController`. Go uses `context.Context` cancellation instead.                                               |
| `agent.reset()`         | Clear all messages, queues, and error state.                                                                                        |
| `agent.waitForIdle()`   | Promise that resolves when the agent finishes. Go: `stream.Result()` blocks until done.                                             |


## Go-Only Features

### AI Features


| Feature                    | Description                                                                                                                       |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `GenerateObject[T]()`      | Top-level generic structured output — derives JSON schema from Go types automatically.                                            |
| `GenerateImage()`          | Top-level image generation function with `ImageProvider` interface.                                                               |
| `ObjectProvider` interface | Optional provider capability for structured output, separate from text streaming.                                                 |
| `ImageProvider` interface  | Optional provider capability for image generation.                                                                                |
| Typed tool I/O             | `ToolDef[In, Out]` with Go generics handles JSON marshal/unmarshal automatically. TS uses TypeBox schemas with manual validation. |


### Agent Features


| Feature                  | Description                                                                                                           |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------- |
| Per-tool `Parallel` flag | Explicit parallel-safe marking on `tool.Info()`. TS uses a global mode.                                               |
| Tool streaming updates   | `OnUpdate func(Result)` callback for progress during tool execution.                                                  |
| Panic recovery           | Per-tool `recover()` converts panics to error tool results.                                                           |
| Multi-subscriber streams | `EventStream` backed by pubsub broker with ring buffer — multiple goroutines can subscribe independently with replay. |
| Immutable construction   | Config frozen at `New()` — safe concurrent reads without locks on config fields.                                      |


