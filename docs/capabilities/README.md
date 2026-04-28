---
title: "Provider Capability Matrix"
summary: "Per-feature compatibility tables comparing upstream provider APIs to pi-go's implementation"
read_when:
  - Choosing a provider for a feature
  - Planning new provider work
  - Auditing pi-go gaps against upstream APIs
---

# Provider Capability Matrix

This folder documents pi-go's coverage of upstream provider features. Each page contains a compatibility matrix, links to official provider documentation, and a list of current gaps in pi-go.

Status as of April 2026. Providers covered:

- **Anthropic** — Messages API ([pkg/ai/provider/anthropic](../../pkg/ai/provider/anthropic))
- **OpenAI Chat** — Chat Completions API ([pkg/ai/provider/openai](../../pkg/ai/provider/openai))
- **OpenAI Responses** — Responses API ([pkg/ai/provider/openairesponses](../../pkg/ai/provider/openairesponses))
- **OpenRouter** — Responses-API dialect of the same package ([openrouter-dialect](../concepts/ai/openrouter-dialect.md))
- **Google** — Gemini API ([pkg/ai/provider/google](../../pkg/ai/provider/google))
- **Claude CLI** — `claude` subprocess ([pkg/ai/provider/claudecli](../../pkg/ai/provider/claudecli))
- **Gemini CLI** — Cloud Code Assist HTTP ([pkg/ai/provider/geminicli](../../pkg/ai/provider/geminicli))

## Pages

### Inputs & Outputs
- [Streaming](streaming.md) — SSE / event-stream consumption
- [Multimodal Input](multimodal-input.md) — images, documents, audio, video
- [Multimodal Output](multimodal-output.md) — image generation, audio (TTS)
- [Citations](citations.md) — inline source attribution
- [Structured Outputs](structured-outputs.md) — JSON mode and schema enforcement

### Tools
- [Function Calling](function-calling.md) — client-side tools, parallel calls, tool choice
- [Server-Side Tools](server-tools.md) — web search/fetch, code execution, computer use, bash, text editor, file search, tool search, MCP

### Reasoning & Optimization
- [Reasoning](reasoning.md) — thinking budgets, encrypted reasoning passthrough
- [Prompt Caching](prompt-caching.md) — TTL, breakpoints, session affinity

### Sampling
- [Sampling Controls](sampling.md) — temperature, top-p/k, penalties, stop, seed, logprobs, safety

### Auth & Lifecycle
- [Authentication](auth.md) — API keys, OAuth, service accounts, cloud endpoints
- [Files API](files-api.md) — uploaded-file references
- [Stateful Conversations](stateful-conversations.md) — provider-managed session state
- [Async Execution](async-execution.md) — Batch API, background mode
- [Realtime / Live API](realtime-api.md) — bidirectional streaming
- [Usage, Cost & Token Counting](usage-and-tokens.md) — response usage, cost calc, pre-request counting
- [Fine-Tuning](fine-tuning.md) — custom-model training

## Legend

Compatibility tables use:

- ✅ supported
- ⚠️ partial / preview / behind a flag
- ❌ not supported
- — not applicable

Where helpful, columns split into **API** (upstream support) and **pi-go** (wired up in this SDK), with caveats and `file:line` references in the Notes column.
