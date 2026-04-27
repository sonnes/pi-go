---
title: "Async Execution"
summary: "Batch API and background mode for long-running or bulk inference"
read_when:
  - Processing a large queue of independent prompts
  - Running deep-research / long agentic flows
---

# Async Execution

## Batch API

Async bulk inference. Submit a JSONL of requests, run within a window (typically 24h, often <1h), retrieve results at ~50% of synchronous cost.

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ Message Batches; up to 300k output tokens with `output-300k-2026-03-24` beta | ❌ | not wired |
| OpenAI Chat | ✅ Batch API; 50% discount | ❌ | not wired |
| OpenAI Responses | ✅ | ❌ | not wired |
| Google Gemini | ✅ via Vertex AI batch predictions | ❌ | not wired |
| Claude CLI | ❌ | — | |
| Gemini CLI | ❌ | — | |

## Background Mode

Long-running async generation: submit, poll status, retrieve.

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ❌ (use Batch instead) | — | |
| OpenAI Chat | ❌ | — | |
| OpenAI Responses | ✅ `background: true` (preview) | ❌ | not wired |
| Google Gemini | ❌ | — | Live API is realtime, not background |

## Provider Documentation

- [Anthropic — Message Batches](https://docs.anthropic.com/en/docs/build-with-claude/batch-processing)
- [OpenAI — Batch API](https://platform.openai.com/docs/guides/batch)
- [OpenAI Responses — Background mode](https://platform.openai.com/docs/guides/background)
- [Google — Vertex AI batch predictions](https://cloud.google.com/vertex-ai/generative-ai/docs/multimodal/batch-prediction-api)

## pi-go Gaps

- No `BatchProvider` capability interface; no JSONL builder, submit/poll/retrieve helpers.
- No `BackgroundProvider` capability interface.
- Both belong as optional capability interfaces alongside [`ImageProvider`](../../pkg/ai/image.go) and [`ObjectProvider`](../../pkg/ai/object.go).
- Without these, callers must use synchronous calls even for embarrassingly parallel workloads — forfeiting the ~50% Batch discount.
