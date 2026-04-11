---
title: "Usage"
summary: "Token usage tracking and cost calculation"
read_when:
  - Tracking token consumption or costs
  - Working with model pricing
---

# Usage

Usage tracks token consumption and cost for model responses.

## Token categories

Four categories: `Input`, `Output`, `CacheRead`, `CacheWrite`, plus a `Total`. Cache categories are provider-specific — not all providers report them.

`CacheRead` and `CacheWrite` are populated automatically on supported providers because [Prompt Caching](/concepts/ai/caching) is on by default. Callers who want byte-exact control over requests can disable it via `ai.WithCacheRetention(ai.CacheRetentionNone)`.

## Cost calculation

`CalculateCost(model, usage)` computes cost as `tokens × model.Cost.{category} / 1,000,000` for each category. All costs are in USD.

## Where usage appears

- **Assistant messages** carry `Usage` with token counts from that response.
- **Agent events** — `agent_end` carries accumulated `Usage` across all turns.
- **Object generation** — `ObjectResult` includes `Usage`.

## Related

- [Models](/concepts/ai/models) — `Cost` defines per-million-token pricing
- [Messages](/concepts/ai/messages) — assistant messages carry `Usage`
- [Agent State](/concepts/agent/agent-state) — agent-level usage tracking
- [Prompt Caching](/concepts/ai/caching) — how `CacheRead` / `CacheWrite` are produced
