---
title: "OpenRouter Dialect"
summary: "Reuses the OpenAI Responses adapter for OpenRouter via a dialect flag, translating server-tool naming and SSE events"
read_when:
  - Wiring OpenRouter as a provider in pi-go
  - Debugging why a server tool succeeds on OpenAI but not on OpenRouter
  - Understanding why two providers can share the same `API()` ID
---

# OpenRouter Dialect

OpenRouter exposes a Responses-API-shaped endpoint at
`https://openrouter.ai/api/v1/responses` that accepts every model in its
catalog (OpenAI, Anthropic, Google, Llama, etc.). The shape is ~90% identical
to OpenAI's native Responses API; pi-go reuses
[`pkg/ai/provider/openairesponses`](../../../pkg/ai/provider/openairesponses)
for both, switched by a dialect flag set at construction time.

```
NewForOpenRouter(...)
        │
        ▼
   Provider{dialect: DialectOpenRouter}
        │
        ├── buildParams ──► params.Tools is left empty
        │
        ├── StreamText  ──► option.WithJSONSet("tools", openrouter-shaped)
        │                     overwrites the body's tools array
        │
        └── SSE switch  ──► response.content_part.delta  → EventTextDelta
                            response.output_item.added with
                              type "openrouter:*"        → EventToolStart
```

## Why a dialect flag, not a separate package

OpenRouter's two divergences from OpenAI Responses are surgical: server-tool
naming (the `openrouter:*` namespace) and a smaller SSE event taxonomy that
emits `response.content_part.delta` instead of `response.output_text.delta`
for incremental text. Forking the entire adapter to handle these would
duplicate the ~400 lines of message conversion, parameter building, tool
choice mapping, and streaming bookkeeping that are identical across the two.
A flag on the existing `Provider` is the smallest change that captures the
gap honestly.

## Tool naming

OpenRouter's typed union `responses.ToolUnionParam` cannot express the
`openrouter:*` shape. The dialect sidesteps the SDK's typed tools entirely:
[`convertOpenRouterTools`](../../../pkg/ai/provider/openairesponses/convert.go)
emits a plain `[]map[string]any`, and `StreamText` injects it into the
request body via
[`option.WithJSONSet("tools", ...)`](https://pkg.go.dev/github.com/openai/openai-go/option#WithJSONSet).
Three server-tool types map cleanly today — `web_search`, `web_fetch`,
`datetime` — and the rest are dropped silently per the existing convention.

## Both dialects share `API() == "openai-responses"`

The dialect is a transport-level detail, not a separate API surface. Two
`Provider` instances may share the same API ID; pi-go's global registry is
keyed by API ID, so callers must bind a specific provider per agent via
`agent.WithProvider(p)` rather than relying on `ai.GetProvider(model.API)`.
This matches how downstream projects already wire per-agent providers.

## Recording cassettes

End-to-end tests live in
[`openrouter_test.go`](../../../pkg/ai/provider/openairesponses/openrouter_test.go)
and depend on `httprr` cassettes. Record once with the live API:

```
OPENROUTER_API_KEY=... go test -httprecord=TestOpenRouter \
    ./pkg/ai/provider/openairesponses/
```

Subsequent runs replay the cassettes deterministically; missing cassettes
cause the tests to skip with a recording hint.
