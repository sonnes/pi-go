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

Models are value types with no methods and no provider identity — pure intrinsic metadata. This keeps them serializable, free from provider dependencies, and bindable to any provider that serves them: `ai.NewLanguageModel(model, provider)` produces the callable unit, and the [catalog](/concepts/ai/providers) does the same binding when resolving a spec string.

## Identification

- `ID` is the canonical identifier sent to the provider (e.g. `"claude-sonnet-4-6"`).
- `Aliases` register extra lookup specs in a catalog, `"<provider>/<alias>"`.

## Catalog and lookup

Provider identity lives in `catalog.Catalog`, not on the model: registering a provider ingests every model it serves, keyed by `"<provider>/<ID>"` (plus one per alias). Each provider package ships a generated model table (`make gen`, sourced from models.dev), so registering a provider is enough to make its models resolvable.

`catalog.GenerateText(ctx, "anthropic-messages/claude-sonnet-4-6", prompt)` resolves the spec to a bound model and runs it, so callers can name a model by string instead of constructing one. `StreamText`, `GenerateObject[T]`, `GenerateImage`, and `GenerateSpeech` share the same spec-first form, so every modality is reached the same way — both on a `Catalog` and as package-level helpers in `pi`.

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
