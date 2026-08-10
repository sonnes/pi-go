---
title: "Providers"
summary: "Provider capabilities, typed registration, and built-in providers"
read_when:
  - Implementing a custom provider
  - Understanding how models route to providers
---

# Providers

A provider implementation supplies one or more transport capabilities between
the SDK and an AI service.

## Design: registry + small interface

Applications register providers under an explicit id such as `"anthropic-messages"` or `"openai-completions"`. Model metadata and provider behavior stay independent, so the same provider instance can be registered under an application-selected identity.

The core capability interface is intentionally small: `StreamText()` for execution. Identity is not part of the capability. Built-in packages expose their default identity as `ID` and their static metadata through `Models()`. Everything goes through streaming — `GenerateText` is built on top by collecting the stream.

## Optional capabilities

Providers can also implement `ImageProvider` or `SpeechProvider` and register those capabilities independently. `ObjectProvider` remains an optional upgrade of a registered text provider. This avoids forcing providers to stub out methods they do not support.

## Registry

`catalog.Catalog` has separate text, image, and speech provider lookups. The capabilities share one model index keyed by `"<provider>/<id>"` and aliases. Each registration accepts its models as variadic arguments:

```go
p := openai.New(opts...)
models := openai.Models()

c := catalog.New()
c.RegisterTextProvider(openai.ID, p, models...)
c.RegisterImageProvider(openai.ID, p)
```

Models discovered later can still be added with `RegisterModel`. `catalog.New()` creates isolated instances for tests, and `pi.Default` is the process-wide catalog behind the `pi` helpers.

## How models find providers

A full spec resolves metadata from the shared model index and then selects the matching capability provider by its prefix. A bare model id resolves only when one registered provider id serves it. There is no fallback or provider guessing.

## Built-in providers

| Package                              | Provider ID              | Service                    |
| ------------------------------------ | ------------------------ | -------------------------- |
| `pkg/ai/provider/anthropic`         | `"anthropic-messages"`   | Anthropic Messages API     |
| `pkg/ai/provider/openai`            | `"openai-completions"`   | OpenAI Chat Completions    |
| `pkg/ai/provider/openairesponses`   | `"openai-responses"`     | OpenAI Responses API       |
| `pkg/ai/provider/google`            | `"google-generative"`    | Google AI (Gemini)         |
| `pkg/ai/provider/claudecli`         | `"claude-cli"`           | Claude CLI subprocess      |
| `pkg/ai/provider/codexcli`          | `"codex-cli"`            | Codex CLI subprocess       |
| `pkg/ai/provider/cursorcli`         | `"cursor-cli"`           | Cursor CLI subprocess      |

Each provider handles request/response conversion between the SDK's types and the provider's native API format.

## Prompt caching

Built-in providers participate in prompt caching at different levels. Anthropic receives explicit `cache_control` markers on the system prompt and the last message's final block. OpenAI Chat and OpenAI Responses receive a `prompt_cache_key` derived from `StreamOptions.SessionID` and rely on automatic server-side prefix matching. Google caches implicitly and reports hits via `CacheRead`. The Claude CLI, Codex CLI, and Cursor CLI providers inherit whatever caching the underlying CLI or backend does. See [Prompt Caching](./caching.md) for the placement rule and per-provider details.

## Authentication

All SDK-based providers authenticate via API keys passed at construction time (`WithAPIKey` or equivalent). Anthropic and OpenAI Chat also support OAuth with automatic token refresh:

| Provider    | Package      | OAuth constructor                                   | Client credentials required  |
| ----------- | ------------ | --------------------------------------------------- | ---------------------------- |
| Anthropic   | `anthropic`  | `WithOAuth(clientID, creds, ...opts)`               | Client ID                    |
| OpenAI      | `openai`     | `NewWithOAuth(clientID, creds, ...opts)`            | Client ID                    |

See [OAuth](../auth/oauth.md) for details on the transport layer and token refresh design.

The Claude CLI, Codex CLI, and Cursor CLI providers delegate authentication entirely to their subprocesses. They inherit whatever credentials those CLIs have configured.

## Related

- [Models](./models.md) — model metadata and binding
- [Options](./options.md) — `StreamOptions` passed to providers
- [OAuth](../auth/oauth.md) — optional OAuth transport middleware
- [Streaming](../agent/streaming.md) — agent event subscription and lifecycle
- [Prompt Caching](./caching.md) — cross-provider cache markers and session affinity
