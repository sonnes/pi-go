---
title: "Sampling Controls"
summary: "Temperature, top-p/k, penalties, stop sequences, seed, logprobs, safety"
read_when:
  - Tuning generation behavior for a feature
  - Building eval harnesses that need determinism
---

# Sampling Controls

Today [`StreamOptions`](../../pkg/ai/options.go) exposes only `Temperature` and `MaxTokens`. Most other sampling knobs are unwired even where the provider supports them.

## Core Sampling Parameters

| Parameter | Anthropic | OpenAI Chat | OpenAI Responses | Google | pi-go option |
|---|---|---|---|---|---|
| `temperature` | ✅ | ✅ | ✅ | ✅ | ✅ ([options.go](../../pkg/ai/options.go)) |
| `max_tokens` | ✅ | ✅ `max_completion_tokens` | ✅ `max_output_tokens` | ✅ `maxOutputTokens` | ✅ |
| `top_p` | ❌ | ✅ | ✅ | ✅ | ❌ |
| `top_k` | ❌ | ❌ | ❌ | ✅ | ❌ |
| `frequency_penalty` | ❌ | ✅ | ✅ | ❌ | ❌ |
| `presence_penalty` | ❌ | ✅ | ✅ | ❌ | ❌ |

OpenAI reasoning models lock temperature; pi-go gates this through `compat.SupportsTemperature` ([openai.go:304-305](../../pkg/ai/provider/openai/openai.go#L304)). `MaxTokens` is forced upward when thinking is enabled on Anthropic ([anthropic.go:256](../../pkg/ai/provider/anthropic/anthropic.go#L256)) — caller's request is silently overridden.

## Stop Sequences

| Provider | API | pi-go |
|---|---|---|
| Anthropic | ✅ `stop_sequences` | ❌ |
| OpenAI Chat | ✅ `stop` | ❌ |
| OpenAI Responses | ✅ | ❌ |
| Google Gemini | ✅ `stopSequences` | ❌ |

## Seed / Reproducibility

| Provider | API | pi-go |
|---|---|---|
| Anthropic | ❌ | — |
| OpenAI Chat | ✅ `seed` + returns `system_fingerprint` | ❌ |
| OpenAI Responses | ✅ | ❌ |
| Google Gemini | ❌ | — |

## Logprobs

| Provider | API | pi-go |
|---|---|---|
| Anthropic | ❌ | — |
| OpenAI Chat | ✅ `logprobs` + `top_logprobs` | ❌ |
| OpenAI Responses | ✅ | ❌ |
| Google Gemini | ⚠️ `responseLogprobs` (preview) | ❌ |

## Safety / Moderation

| Provider | API | pi-go |
|---|---|---|
| Anthropic | ⚠️ policy-driven; no per-call thresholds | ❌ |
| OpenAI Chat | ⚠️ built-in filter; separate Moderation API | ❌ |
| OpenAI Responses | ⚠️ same | ❌ |
| Google Gemini | ✅ `safetySettings` per harm category (HARASSMENT, HATE_SPEECH, SEXUALLY_EXPLICIT, DANGEROUS_CONTENT, CIVIC_INTEGRITY) with thresholds | ❌ |

## Provider Documentation

- [Anthropic — Messages params](https://docs.anthropic.com/en/api/messages)
- [OpenAI — Chat completions params](https://platform.openai.com/docs/api-reference/chat/create)
- [OpenAI — Responses params](https://platform.openai.com/docs/api-reference/responses/create)
- [OpenAI — Reproducible outputs (`seed`)](https://platform.openai.com/docs/api-reference/chat/create#chat-create-seed)
- [OpenAI — Logprobs](https://platform.openai.com/docs/api-reference/chat/create#chat-create-logprobs)
- [OpenAI — Moderation API](https://platform.openai.com/docs/guides/moderation)
- [Google Gemini — generationConfig](https://ai.google.dev/api/generate-content#generationconfig)
- [Google Gemini — Safety settings](https://ai.google.dev/gemini-api/docs/safety-settings)

## pi-go Gaps

- **`top_p`, `top_k`, `frequency_penalty`, `presence_penalty`** not exposed for any provider that supports them.
- **Stop sequences** missing entirely; `StopReason` has no value distinguishing "matched stop sequence" from regular stop.
- **`seed`** missing; no `SystemFingerprint` on returned messages.
- **Logprobs**: no content type or message field for per-token logprobs. Adding requires extending [`ai.Text`](../../pkg/ai/content.go) or a parallel metadata field.
- **Safety settings**: no `SafetySettings` option; no way to surface a `safety_ratings` / blocked-output reason from Gemini responses; `StopReason` has no `safety` value.
- **`MaxTokens` override** when thinking is on (Anthropic) is silent — caller's intent isn't preserved.
