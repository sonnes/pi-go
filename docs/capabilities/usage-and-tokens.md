---
title: "Usage, Cost, and Token Counting"
summary: "Per-call usage extraction, cost calculation, and pre-request token counting endpoints"
read_when:
  - Surfacing per-call cost
  - Building cost previews or context-window guards
  - Implementing usage extraction for a new provider
---

# Usage, Cost, and Token Counting

[`ai.Usage`](../../pkg/ai/usage.go) tracks `Input`, `Output`, `CacheRead`, `CacheWrite`, `Total` token counts plus a `UsageCost` breakdown in USD. [`ai.CalculateCost(model, usage)`](../../pkg/ai/usage.go) multiplies usage by `Model.Cost` (per-million-token rates).

## Usage / Cost from Responses

| Provider | API fields | pi-go extraction | Notes |
|---|---|---|---|
| Anthropic | input, output, cache_read_input_tokens, cache_creation_input_tokens, cache_creation by TTL | ✅ all four token kinds | ([anthropic.go:322-329](../../pkg/ai/provider/anthropic/anthropic.go#L322)); cost via `CalculateCost` |
| OpenAI Chat | prompt_tokens, completion_tokens, total_tokens, prompt_tokens_details.cached_tokens, reasoning_tokens | ⚠️ collapses to `TotalTokens` only ([openai.go:94](../../pkg/ai/provider/openai/openai.go#L94)) | no input/output split, no cache, no reasoning |
| OpenAI Responses | input, output, total, input.cached_tokens, output.reasoning_tokens | ✅ input/output/total + cache_read | ([convert.go:285](../../pkg/ai/provider/openairesponses/convert.go#L285)); reasoning tokens not extracted |
| Google Gemini | promptTokenCount, candidatesTokenCount, totalTokenCount, cachedContentTokenCount, thoughtsTokenCount | ⚠️ partial | `mapUsage` extracts input/output/total only |
| Claude CLI | available in NDJSON | ❌ | discarded |
| Gemini CLI | available on response | ❌ | discarded |

## Pre-Request Token Counting

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ `/v1/messages/count_tokens` (free, separate rate limits) | ❌ | not wired |
| OpenAI Chat | ❌ | — | use `tiktoken` locally |
| OpenAI Responses | ✅ | ❌ | not wired |
| Google Gemini | ✅ `countTokens` (free) | ❌ | not wired |

## Provider Documentation

- [Anthropic — Usage in responses](https://docs.anthropic.com/en/api/messages#response-usage)
- [Anthropic — Token counting](https://docs.anthropic.com/en/api/messages-count-tokens)
- [OpenAI — Usage](https://platform.openai.com/docs/api-reference/chat/object#chat/object-usage)
- [Google Gemini — UsageMetadata](https://ai.google.dev/api/generate-content#usagemetadata)
- [Google Gemini — `countTokens`](https://ai.google.dev/api/tokens)

## pi-go Gaps

- **OpenAI Chat** input/output split not surfaced — cost calculation can't run accurately.
- **Reasoning tokens** (OpenAI Responses, Gemini `thoughtsTokenCount`) not surfaced.
- **Gemini cache hit count** not extracted.
- **Claude CLI / Gemini CLI** drop usage entirely.
- `Model.Cost` map needs population per model — not all models in the registry have pricing.
- **No `TokenCounter` interface** — no pre-request token counting helper anywhere; local tokenizer (e.g. `tiktoken-go`) not bundled either.
