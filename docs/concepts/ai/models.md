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

Models are value types with no methods. This keeps them serializable, comparable, and free from provider dependencies. The `Provider` field is the bridge — it names the registered provider that handles the model and namespaces the model in the registry.

## Identification

- `ID` is the canonical identifier sent to the provider (e.g. `"claude-sonnet-4-6"`).
- `Provider` names the registered [Provider](/concepts/ai/providers) that handles requests (e.g. `"anthropic-messages"`) and forms the model's registry spec, `"<Provider>/<ID>"`.
- `Aliases` register extra lookup specs, `"<Provider>/<alias>"`.

## Registry and lookup

Models live in `Registry` alongside providers, keyed by their `"<Provider>/<ID>"` spec (plus one per alias). Register with `RegisterModel`, look up with `ResolveModel`, list with `Models` — a package-level default backs these, and `NewRegistry()` gives isolated instances. Callers populate the registry; pi-go ships no built-in model table.

`ai.Generate(ctx, "anthropic-messages/claude-sonnet-4-6", prompt)` resolves the spec to a `Model` and runs it, so callers can name a model by string instead of constructing one. `ai.Stream`, `ai.GenerateObject[T]`, and `ai.GenerateImage` share the same spec-first form, so every modality is reached the same way.

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
