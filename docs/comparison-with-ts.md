# Comparison with pi-mono/packages/ai (TypeScript)

This document tracks the API surface differences between the Go SDK (`pi-go`) and the TypeScript SDK (`pi-mono/packages/ai`).

## Core Abstractions — Aligned

These areas have strong parity between the two SDKs:

| Concept | TypeScript | Go |
|---------|-----------|-----|
| Message roles | `"user"`, `"assistant"`, `"toolResult"` | `RoleUser`, `RoleAssistant`, `RoleToolResult` |
| Content blocks | `TextContent`, `ThinkingContent`, `ImageContent`, `ToolCall` | `Text`, `Thinking`, `Image`, `ToolCall` |
| Streaming events | `text_start/delta/end`, `thinking_start/delta/end`, `toolcall_start/delta/end`, `start`, `done`, `error` | Same event types |
| EventStream | Async iterable + `.result()` promise | `Events() iter.Seq2` + `Result() (*Message, error)` |
| Provider interface | `ApiProvider { api, stream, streamSimple }` | `Provider { API(), StreamText() }` |
| Provider registry | `registerApiProvider()` / `getApiProvider()` / `clearApiProviders()` | `RegisterProvider()` / `GetProvider()` / `ClearProviders()` |
| Tool definition | `Tool<TParameters>` with TypeBox schema | `ToolDef[In, Out]` with jsonschema |
| Model struct | `Model<TApi>` with id, name, api, provider, costs, etc. | `Model` struct with same fields |
| Prompt / Context | `Context { systemPrompt, messages, tools }` | `Prompt { System, Messages, Tools }` |
| Stop reasons | `"stop"`, `"length"`, `"toolUse"`, `"error"`, `"aborted"` | `StopReasonStop`, `StopReasonLength`, `StopReasonToolUse`, `StopReasonError`, `StopReasonAborted` |
| ThinkingLevel | `"minimal"` / `"low"` / `"medium"` / `"high"` / `"xhigh"` | Same values |
| ToolChoice | auto / none / required + specific tool | Same |
| Usage tracking | input, output, cacheRead, cacheWrite, totalTokens, cost | Same fields |
| Providers | Anthropic, OpenAI, Google (+ many more) | Anthropic, OpenAI, Google |

## API Design Differences

These are intentional divergences driven by language idioms:

| Area | TypeScript | Go | Rationale |
|------|-----------|-----|-----------|
| Naming | `Context` for prompt container | `Prompt` | Avoids collision with `context.Context` |
| Options | Plain objects with spreads | Functional options (`WithTemperature()`, etc.) | Idiomatic Go pattern |
| Error handling | try/catch + error events | `error` returns + `EventError` | Go error conventions |
| Generics | TypeBox schemas at runtime | `GenerateObject[T]`, `ToolDef[In, Out]` | Go generics enable compile-time type safety |
| Provider dispatch | Per-provider stream functions + registry | Interface-based dispatch via registry only | Go interface polymorphism |
| stream vs streamSimple | `stream()` (raw options) vs `streamSimple()` (unified reasoning) | Single `StreamText()` with `ThinkingLevel` in options | Go combines both paths — simpler surface |
| Abort / cancellation | `AbortSignal` on options | `context.Context` cancellation | Native Go cancellation |

## Missing from Go

### Providers

| Provider | TS API identifier | Status |
|----------|------------------|--------|
| OpenAI Responses | `openai-responses` | Not implemented |
| Azure OpenAI Responses | `azure-openai-responses` | Not implemented |
| OpenAI Codex Responses | `openai-codex-responses` | Not implemented |
| Mistral | `mistral-conversations` | Not implemented |
| AWS Bedrock | `bedrock-converse-stream` | Not implemented |
| Google Vertex | `google-vertex` | Not implemented |
| Google Gemini CLI | `google-gemini-cli` | Not implemented |

### Features

| Feature | Description |
|---------|-------------|
| Model registry | TS has `getModel()`, `getModels()`, `getProviders()` with a generated model database. Go requires users to construct `Model` structs manually. |
| OAuth | TS has full OAuth with PKCE for Anthropic, GitHub Copilot, Google CLI, etc. |
| `CacheRetention` | `"none"` / `"short"` / `"long"` option for prompt caching hints. |
| `Transport` | `"sse"` / `"websocket"` / `"auto"` transport selection. |
| `transformMessages()` | Cross-provider message normalization: drops incompatible thinking blocks, fixes tool call IDs, inserts synthetic tool results for orphaned calls. |
| `isContextOverflow()` | Context overflow detection with regex patterns. |
| `parseStreamingJson()` | Partial JSON parsing for incomplete tool call arguments during streaming. |
| `validateToolCall()` | Runtime AJV validation of tool arguments against schema. |
| `ThinkingBudgets` | Custom token allocations per thinking level. Go hardcodes budgets per provider. |
| `onPayload` | Raw payload inspection callback on stream options. |
| `sessionId` | Session tracking option. |
| Provider routing | OpenRouter / Vercel AI Gateway routing configuration. |
| `sanitizeSurrogates()` | Unicode surrogate cleaning utility. |
| `getEnvApiKey()` | Environment variable resolution for API keys by provider name. |

## Go-Only Features

Features present in the Go SDK that the TypeScript SDK does not have:

| Feature | Description |
|---------|-------------|
| `GenerateObject[T]()` | Top-level generic structured output — derives JSON schema from Go types automatically. |
| `GenerateImage()` | Top-level image generation function with `ImageProvider` interface. |
| `ObjectProvider` interface | Optional provider capability for structured output, separate from text streaming. |
| `ImageProvider` interface | Optional provider capability for image generation. |
| Typed tool I/O | `ToolDef[In, Out]` with Go generics handles JSON marshal/unmarshal automatically. TS uses TypeBox schemas with manual validation. |
| Tool `Parallel` flag | Explicit parallel-safe marking on `tool.Info`. |
| Tool streaming updates | `OnUpdate func(Result)` callback for progress during tool execution. |
