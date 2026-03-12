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

| Package                       | API          | Service   |
| ----------------------------- | ------------ | --------- |
| `pkg/ai/provider/anthropic`  | `"anthropic"`| Anthropic |
| `pkg/ai/provider/openai`     | `"openai"`   | OpenAI    |
| `pkg/ai/provider/google`     | `"google"`   | Google AI |

Each provider handles request/response conversion between the SDK's types and the provider's native API format.

## Related

- [Models](/concepts/ai/models) — model metadata and the `API` field
- [Options](/concepts/ai/options) — `StreamOptions` passed to providers
- [Streaming](/concepts/agent/streaming) — `EventStream` returned by providers
