---
title: "Prompt Caching"
summary: "Reusing KV cache for repeated prefixes — TTL, breakpoints, session affinity"
read_when:
  - Optimizing cost / latency for repeat prefixes
  - Adding caching to a new provider
---

# Prompt Caching

See also the design-level concept doc at [docs/concepts/ai/caching.md](../concepts/ai/caching.md). pi-go controls caching with [`StreamOptions.CacheRetention`](../../pkg/ai/options.go) (`none`, `short`, `long`) and a `SessionID` for affinity.

## Compatibility

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ `cache_control` markers; 5m default + 1h `ttl` (extended TTL beta); up to 4 breakpoints | ⚠️ | terminal auto-breakpoint only ([cache.go:50-74](../../pkg/ai/provider/anthropic/cache.go#L50)); 1h only on api.anthropic.com ([cache.go:29-30](../../pkg/ai/provider/anthropic/cache.go#L29)) |
| OpenAI Chat | ✅ automatic; `prompt_cache_key` for affinity | ✅ | session ID forwarded ([openai.go:323-326](../../pkg/ai/provider/openai/openai.go#L323)) |
| OpenAI Responses | ✅ automatic | ✅ | session ID forwarded ([openairesponses.go:339-340](../../pkg/ai/provider/openairesponses/openairesponses.go#L339)) |
| Google Gemini | ✅ context caching API | ❌ | not wired |
| Claude CLI | ⚠️ session-level KV | ❌ | |
| Gemini CLI | ❌ | — | |

## Provider Documentation

- [Anthropic — Prompt caching](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching)
- [OpenAI — Prompt caching](https://platform.openai.com/docs/guides/prompt-caching)
- [Google Gemini — Context caching](https://ai.google.dev/gemini-api/docs/caching)

## pi-go Gaps

- **Manual breakpoints** for Anthropic (system prompt, tool definitions, prior turns) — pi-go places exactly one terminal marker.
- **Google context caching** not implemented at all (no `cachedContent` reference).
- **Cache hit metrics** for OpenAI Chat are not surfaced — streaming `usage` collapses to a single `TotalTokens` field ([openai.go:94](../../pkg/ai/provider/openai/openai.go#L94)).
- **Anthropic cache write tier** — pi-go does not let callers choose 1h cache writes selectively (it's tied to `CacheRetention`).
