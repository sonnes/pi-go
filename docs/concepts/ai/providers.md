---
title: "Providers"
summary: "Provider interface, registry, optional capabilities, and built-in providers"
read_when:
  - Implementing a custom provider
  - Understanding how models route to providers
---

# Providers

A `Provider` implements the transport layer between the SDK and an AI service.

## Design: registry + small interface

Providers register themselves by API identifier (e.g. `"anthropic"`, `"openai"`). A model's `API` field determines which provider handles it at call time. This decouples model definitions from provider implementations — you can define models in config files without importing provider packages.

The core interface is intentionally small: `API()` for identification and `StreamText()` for execution. Everything goes through streaming — `GenerateText` is built on top by collecting the stream.

## Optional capabilities

Providers can optionally implement `ImageProvider` (image generation) or `ObjectProvider` (structured output). The SDK checks via type assertion at call time. This avoids forcing all providers to stub out methods they don't support.

## Registry

The registry is global, thread-safe, and typically populated at init time via `RegisterProvider`. `ClearProviders()` is available for test isolation.

## How models find providers

A model's `API` field is looked up in the registry. If no provider is registered for that API, an error is returned immediately — no fallback or guessing.

## Built-in providers

| Package                              | API                      | Service                    |
| ------------------------------------ | ------------------------ | -------------------------- |
| `pkg/ai/provider/anthropic`         | `"anthropic-messages"`   | Anthropic Messages API     |
| `pkg/ai/provider/openai`            | `"openai-completions"`   | OpenAI Chat Completions    |
| `pkg/ai/provider/openairesponses`   | `"openai-responses"`     | OpenAI Responses API       |
| `pkg/ai/provider/google`            | `"google-generative"`    | Google AI (Gemini)         |
| `pkg/ai/provider/geminicli`         | `"google-gemini-cli"`    | Cloud Code Assist (Gemini) |
| `pkg/ai/provider/claudecli`         | `"claude-cli"`           | Claude CLI subprocess      |

Each provider handles request/response conversion between the SDK's types and the provider's native API format.

## Prompt caching

Built-in providers participate in prompt caching at different levels. Anthropic receives explicit `cache_control` markers on the system prompt and the last message's final block. OpenAI Chat and OpenAI Responses receive a `prompt_cache_key` derived from `StreamOptions.SessionID` and rely on automatic server-side prefix matching. Google caches implicitly and reports hits via `CacheRead`. The Claude CLI and Gemini CLI providers inherit whatever caching the underlying subprocess does. See [Prompt Caching](/concepts/ai/caching) for the placement rule and per-provider details.

## Authentication

All SDK-based providers authenticate via API keys passed at construction time (`WithAPIKey` or equivalent). Three providers additionally support OAuth with automatic token refresh:

| Provider    | Package      | OAuth constructor                                   | Client credentials required  |
| ----------- | ------------ | --------------------------------------------------- | ---------------------------- |
| Anthropic   | `anthropic`  | `WithOAuth(clientID, creds, ...opts)`               | Client ID                    |
| OpenAI      | `openai`     | `NewWithOAuth(clientID, creds, ...opts)`            | Client ID                    |
| Gemini CLI  | `geminicli`  | `WithOAuth(clientID, clientSecret, creds, ...opts)` | Client ID + Client Secret    |

See [OAuth](/concepts/auth/oauth) for details on the transport layer and token refresh design.

The Claude CLI provider delegates authentication entirely to the subprocess — it inherits whatever credentials the CLI has configured (OAuth session, API key, or credential helper).

## Related

- [Models](/concepts/ai/models) — model metadata and the `API` field
- [Options](/concepts/ai/options) — `StreamOptions` passed to providers
- [OAuth](/concepts/auth/oauth) — optional OAuth transport middleware
- [Streaming](/concepts/agent/streaming) — agent event subscription and lifecycle
- [Prompt Caching](/concepts/ai/caching) — cross-provider cache markers and session affinity
