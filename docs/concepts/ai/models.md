---
title: "Models"
summary: "Model struct, identification, modalities, cost, and provider compatibility"
read_when:
  - Adding or configuring a model
  - Working with model metadata or pricing
---

# Models

A `Model` describes an AI model and its capabilities. Models are plain structs — they carry metadata but don't execute anything. Execution happens through [Providers](/concepts/ai/providers).

## Design: data, not behavior

Models are value types with no methods. This keeps them serializable, comparable, and free from provider dependencies. The `API` field is the bridge — it selects which registered provider handles requests for the model at call time.

## Identification

- `ID` is the canonical identifier sent to the provider API (e.g. `"claude-sonnet-4-20250514"`).
- `API` selects which registered [Provider](/concepts/ai/providers) handles requests.
- `Aliases` allow alternative names for model lookup.

## Modalities

`Input` and `Output` describe what the model accepts and produces (`text`, `image`). These are advisory — providers may reject unsupported modality combinations at call time.

## Cost

`Cost` defines per-million-token pricing in USD. `CalculateCost(model, usage)` computes the cost breakdown for a response. See [Usage](/concepts/ai/usage).

## Provider compatibility

`ProviderCompat` is an optional interface for provider-specific compatibility metadata (excluded from JSON). Providers that need cross-compatible model handling implement this interface — for example, mapping between different tool calling conventions.

## Related

- [Providers](/concepts/ai/providers) — provider registration and the `Provider` interface
- [Usage](/concepts/ai/usage) — token usage and cost tracking
- [Options](/concepts/ai/options) — per-request configuration
