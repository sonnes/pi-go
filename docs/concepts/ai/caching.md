---
title: "Prompt Caching"
summary: "Cross-provider cache_control markers, retention levels, session affinity"
read_when:
  - Enabling or disabling prompt caching
  - Adding caching support to a new provider adapter
  - Debugging why cache hits aren't happening
---

# Prompt Caching

Prompt caching lets a provider reuse the KV cache of a previously processed prefix instead of recomputing it from scratch. For repeated system prompts, long tool definitions, or multi-turn conversations, this is an order-of-magnitude reduction in latency and cost on cache hits. pi-go enables caching automatically on supported providers.

## Design: default on, terminal breakpoint

Caching is on by default. Callers who never set an option still get cache hits — no opt-in required. The behavior is controlled by [`CacheRetention`](/concepts/ai/options), which has four values:

- `CacheRetentionDefault` (zero value) — resolves to `Short`.
- `CacheRetentionShort` — provider's default ephemeral TTL (Anthropic: 5 minutes).
- `CacheRetentionLong` — longer ephemeral TTL where supported (Anthropic: 1 hour on `api.anthropic.com`).
- `CacheRetentionNone` — disable markers entirely for this request.

A single helper, `ai.ResolveCacheRetention`, centralizes the "default is Short" rule so provider adapters never drift on it.

## Design: terminal-breakpoint placement

pi-go places **at most two** cache breakpoints per request:

1. One on the system prompt.
2. One on the final content block of the last message in the conversation.

The marker carries the semantics "everything before this point is cacheable." On the next turn the previous terminal block is now interior to the prefix and still matches the cached bytes, so a single static placement rule delivers turn-over-turn hits without the agent tracking where it put the marker last time.

**Why not also mark tools.** Tool schemas are part of the prefix that gets cached automatically once any downstream marker exists. Anthropic caps `cache_control` at 4 breakpoints per request, so marking tools would spend a slot for no additional benefit.

**Why not split the system prompt.** Claude Code subdivides system prompts into static/dynamic blocks so a volatile suffix can't invalidate a stable prefix. That's a first-party optimization that needs callers to annotate which parts of the system prompt change per turn. pi-go treats the system prompt as a single block and leaves the split strategy to the caller (who can join multiple system chunks themselves if they want finer control).

## Per-provider behavior

| Provider             | Mechanism                                              | Controlled by                                    |
| -------------------- | ------------------------------------------------------ | ------------------------------------------------ |
| Anthropic Messages   | Native `cache_control: {type: "ephemeral", ttl?}` blocks | `CacheRetention` (injection) + base URL gate (TTL) |
| OpenAI Chat          | Automatic prefix match + `prompt_cache_key` affinity   | `SessionID` (affinity only; caching is automatic) |
| OpenAI Responses     | Same as OpenAI Chat                                    | `SessionID`                                      |
| Google (Gemini)      | Fully implicit, server-managed                         | Not configurable; hits reported via `CacheRead` |

Anthropic is the only first-party provider in pi-go where the SDK emits markers. OpenAI's caching is automatic once requests share a prefix; pi-go forwards `StreamOptions.SessionID` as `prompt_cache_key` to strengthen cross-request affinity but never emits block-level markers. Google manages caching server-side and doesn't expose a client control.

The 1h TTL for Anthropic is only attached when the client talks to `api.anthropic.com` directly. Proxies and compatible endpoints receive the marker without a TTL field so the request still serializes cleanly against third-party servers that may not understand the extension.

## Usage tracking

Cache hits and writes are reported in [`ai.Usage`](/concepts/ai/usage) as separate `CacheRead` and `CacheWrite` token counts. `CalculateCost` multiplies them by `Model.Cost.CacheRead` / `Model.Cost.CacheWrite` so cost breakdowns distinguish the three tiers (fresh input, cache write, cache read). Anthropic reports both read and write tokens; OpenAI reports only cache reads via `prompt_tokens_details.cached_tokens`; Google reports cache reads via `cachedContentTokenCount`.

## Adding markers in a new provider adapter

The Anthropic adapter in `pkg/ai/provider/anthropic` is the canonical example. When wiring up a new provider:

1. Call `ai.ResolveCacheRetention(opts.CacheRetention)` at the top of the request builder.
2. Short-circuit when the result is `CacheRetentionNone` — emit no markers, no `prompt_cache_key`.
3. Compute provider-specific TTL only when the client's configured base URL matches the official endpoint. Proxies get the marker without TTL, or no TTL at all, depending on the provider.
4. Build one marker value and attach it to the system prompt block.
5. Walk the converted messages, find the last content block of the last message, attach the same marker. For a union type, branch on whichever content type is populated (text, tool-result, image, etc.).
6. Extract `CacheRead` / `CacheWrite` from the response usage into `ai.Usage` so cost calculation works.

## Session affinity

`StreamOptions.SessionID` (set via `ai.WithSessionID`) provides a stable identifier for cache affinity. Today it only matters for OpenAI — both the Chat Completions and Responses adapters forward it as `prompt_cache_key`. Other providers ignore it. Auto-generation is opt-in: callers create a UUID and pass it themselves. When `CacheRetentionNone` is set, `SessionID` is suppressed too so a client can fully turn off any cache-related wire-level fields.

Claude Code's approach of pure prefix matching (no session ID) works because OpenAI's cache key is only an affinity hint — byte-identical requests still hit the cache without it. The session ID is a strengthening signal, not a requirement.

## Disabling

Pass `ai.WithCacheRetention(ai.CacheRetentionNone)` on a specific call, or thread it through an agent via `agent.WithStreamOpts(ai.WithCacheRetention(ai.CacheRetentionNone))`. There is no environment variable; all control is through the options API.

## Related

- [Options](/concepts/ai/options) — `WithCacheRetention`, `WithSessionID`
- [Usage](/concepts/ai/usage) — `CacheRead` / `CacheWrite` token tracking
- [Providers](/concepts/ai/providers) — which providers are supported
