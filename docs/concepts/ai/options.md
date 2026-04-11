---
title: "Options"
summary: "Per-request configuration: temperature, max tokens, thinking, tool choice"
read_when:
  - Configuring model call parameters
  - Working with thinking levels or tool choice
---

# Options

Options configure individual model calls using the functional options pattern.

## Design: why functional options

Functional options (`WithTemperature`, `WithMaxTokens`, etc.) allow adding new parameters without breaking callers. Options are applied left to right — later options override earlier ones. This pattern is used both for direct `StreamText`/`GenerateText` calls and for agent-level defaults via `WithStreamOpts`.

## Pointer semantics for defaults

`Temperature` and `MaxTokens` are `*float64` and `*int` in the resolved `StreamOptions`. This distinguishes "not set" (nil → provider default) from "set to zero". Providers check for nil and apply their own defaults.

## Thinking levels

For models that support extended reasoning, `WithThinking` sets the thinking depth (`minimal` through `xhigh`). When set, the model produces `Thinking` content blocks alongside text. Not all providers support thinking — unsupported levels are silently ignored or rejected depending on the provider.

## Tool choice

`ToolChoice` controls how the model selects tools: `auto` (model decides, default), `none` (disable), `required` (force at least one), or `SpecificToolChoice(name)` (force a specific tool).

## Cache retention

`WithCacheRetention` controls prompt-cache breakpoint placement and TTL across providers. It is **on by default**: the zero value resolves to `CacheRetentionShort`, so callers get cache hits automatically without opting in. The four values are `CacheRetentionDefault` (unset → Short), `CacheRetentionNone` (disable), `CacheRetentionShort` (provider's default ephemeral TTL), and `CacheRetentionLong` (longer ephemeral TTL where supported).

`WithSessionID` provides cache affinity for providers that support it — currently OpenAI Chat Completions and Responses forward it as `prompt_cache_key`. Other providers ignore it. It is not auto-generated; callers pass a UUID when they want affinity across requests.

See [Prompt Caching](/concepts/ai/caching) for the placement strategy and per-provider behavior.

## Available options

`WithTemperature`, `WithMaxTokens`, `WithThinking`, `WithToolChoice`, `WithCacheRetention`, `WithSessionID`, `WithHeaders`, `WithMetadata`. See GoDoc for signatures.

## Related

- [Models](/concepts/ai/models) — models carry default capabilities; options override per-call
- [Providers](/concepts/ai/providers) — providers receive the resolved `StreamOptions`
- [Content](/concepts/ai/content) — `WithThinking` enables `Thinking` content blocks
- [Prompt Caching](/concepts/ai/caching) — `WithCacheRetention` and `WithSessionID`
