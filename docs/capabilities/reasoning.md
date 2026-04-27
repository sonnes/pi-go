---
title: "Reasoning"
summary: "Extended / adaptive thinking, reasoning effort budgets, encrypted reasoning passthrough"
read_when:
  - Configuring reasoning depth for a feature
  - Maintaining multi-turn reasoning continuity
---

# Reasoning

[`StreamOptions.ThinkingLevel`](../../pkg/ai/options.go) maps a normalized level (`minimal`, `low`, `medium`, `high`, `xhigh`) to each provider's reasoning configuration. Thinking blocks emerge as [`ai.Thinking`](../../pkg/ai/content.go) content with an optional `Signature` for cross-turn passthrough.

## Extended / Adaptive Thinking

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ Adaptive Thinking (default on Opus 4.7) — effort `low/med/high/max`; legacy `extended_thinking` with token budget deprecated | ⚠️ | pi-go still maps level → token budget ([anthropic.go:280-294](../../pkg/ai/provider/anthropic/anthropic.go#L280)); does not use new `effort` knob |
| OpenAI Chat | ✅ `reasoning_effort` on GPT-5.2 / o-series | ✅ | gated by `compat.SupportsReasoningEffort` ([openai.go:308-310](../../pkg/ai/provider/openai/openai.go#L308)) |
| OpenAI Responses | ✅ `reasoning.effort` (`none/low/med/high/xhigh`) + auto summary | ✅ | ([convert.go:290-301](../../pkg/ai/provider/openairesponses/convert.go#L290), [openairesponses.go:318-326](../../pkg/ai/provider/openairesponses/openairesponses.go#L318)) |
| Google Gemini | ✅ `thinkingConfig.thinkingBudget` (Gemini 2.5+); `thinking_level` (Gemini 3+) | ⚠️ | budgets only wired in geminicli ([geminicli.go:206-227](../../pkg/ai/provider/geminicli/geminicli.go#L206)); the `google` HTTP provider does not set `thinkingConfig` |
| Claude CLI | ⚠️ controlled by CLI internally | ⚠️ | not configurable via pi-go |
| Gemini CLI | ✅ | ✅ | full level→budget mapping |

## Encrypted Reasoning Passthrough

Providers return reasoning blocks with a signed/encrypted blob. Clients echo the blob back on subsequent turns; the model decrypts it server-side and continues its prior thinking.

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ thinking signatures | ✅ | round-tripped via `Signature` ([convert.go:307](../../pkg/ai/provider/anthropic/convert.go#L307)) |
| OpenAI Chat | ❌ | — | reasoning kept server-side via stored conversation only |
| OpenAI Responses | ✅ `reasoning.encrypted_content`; ZDR-compatible | ✅ | `ResponseIncludableReasoningEncryptedContent` ([openairesponses.go:323-325](../../pkg/ai/provider/openairesponses/openairesponses.go#L323)) |
| Google Gemini | ✅ `thoughtSignature` (Gemini 3+) | ✅ | preserved on Thinking and ToolCall blocks ([google.go:187, 228-229](../../pkg/ai/provider/google/google.go#L187)) |
| Gemini CLI | ✅ | ✅ | |

## Provider Documentation

- [Anthropic — Extended / Adaptive thinking](https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking)
- [OpenAI — Reasoning models](https://platform.openai.com/docs/guides/reasoning)
- [OpenAI — Encrypted reasoning content](https://platform.openai.com/docs/guides/reasoning#encrypted-reasoning-items)
- [OpenAI — Reasoning best practices](https://platform.openai.com/docs/guides/reasoning-best-practices)
- [Google Gemini — Thinking](https://ai.google.dev/gemini-api/docs/thinking)
- [Google — Thought signatures](https://ai.google.dev/gemini-api/docs/thinking#signatures)

## pi-go Gaps

- **Adaptive Thinking effort levels** (Anthropic, Opus 4.7+) — not used. pi-go still emits the deprecated `budget_tokens` form.
- **Google HTTP provider asymmetry** — `pkg/ai/provider/google` does not set `thinkingConfig`; thinking is uncontrolled there.
- **Reasoning summaries** are wired only for OpenAI Responses; Anthropic and Gemini do not surface summaries.
- **Tools-in-reasoning** chain (OpenAI o3/o4-mini calling tools mid-CoT) — no special handling.
- **Disabling thinking** (level `none` / zero budget) is not consistently expressible across providers.
- Encrypted reasoning passthrough is the strongest area of provider parity — no major gaps there. Verify callers correctly include thinking blocks before tool-result turns or risk losing reasoning state.
