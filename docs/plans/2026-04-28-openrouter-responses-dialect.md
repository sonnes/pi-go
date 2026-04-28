---
title: OpenRouter Dialect for the Responses API Adapter
date: 2026-04-28
status: draft
---

# OpenRouter Dialect for the Responses API Adapter

## Context

OpenRouter (`https://openrouter.ai/api/v1`) exposes a `/responses` endpoint that
accepts the OpenAI Responses request shape across its 300+ routed models. Today
pi-go routes OpenRouter through [`pkg/ai/provider/openai`](../../pkg/ai/provider/openai)
(Chat Completions), so server tools are silently dropped — see
[capabilities/server-tools.md](../capabilities/server-tools.md). We want
OpenRouter clients to use the same code path as OpenAI Responses and get full
server-tool support (web_search, code_interpreter, file_search, computer, mcp)
through a single adapter.

OpenRouter's API is ~90% Responses-shaped, but two surgical differences break
the existing adapter:

1. **Server-tool naming.** OpenRouter requires the `openrouter:*` prefix
   (`openrouter:web_search`, `openrouter:web_fetch`, `openrouter:datetime`,
   `openrouter:image_generation`). OpenAI's typed tools (`web_search_preview`,
   `code_interpreter`, etc.) are rejected.
2. **SSE event taxonomy.** OpenRouter's documented event set is a strict subset
   of OpenAI's: `response.created`, `response.output_item.added`,
   `response.content_part.added`, `response.content_part.delta`,
   `response.output_item.done`, `response.done`. In particular, OpenAI's
   `response.output_text.delta` does not appear; text increments arrive on
   `response.content_part.delta`. Tool-call lifecycle events
   (`response.web_search_call.*`, `response.code_interpreter_call.*`) are not
   documented for OpenRouter and likely absent.

The chosen approach is a **dialect flag** on the existing `openairesponses`
package — smallest viable change, no fork of the adapter.

Empirical gap: OpenRouter's docs are light on SSE payload examples for
server-tool calls. The plan front-loads recording live `httprr` cassettes to
ground every translation rule in observed traffic before writing code.

## Goals

- A single `openairesponses.New(WithOpenRouterDialect())` constructor produces
  a `Provider` that talks to OpenRouter's Responses endpoint correctly.
- Function tools and the full pi-go server-tool surface (`ServerToolWebSearch`,
  `ServerToolWebFetch`, `ServerToolCodeExecution`, `ServerToolFileSearch`,
  `ServerToolComputer`, `ServerToolMCP`, `ServerToolDateTime` if added) all map
  to either an `openrouter:*` tool or are silently dropped per the existing
  convention.
- Streaming yields the same `ai.Event` sequence consumers already expect from
  the OpenAI Responses path (`EventTextStart/Delta/End`, `EventToolStart/End`,
  `EventDone`).
- The `Provider.API()` returned for the dialect is `"openai-responses"` so
  registry-based dispatch in callers (e.g. littlework) is unchanged. Two
  `Provider` instances may share that API ID; callers bind a specific provider
  per agent via `agent.WithProvider(p)` — the path littlework's `factory.go`
  already takes.

## Non-goals

- A standalone `pkg/ai/provider/openrouter` package.
- Wiring `openrouter:image_generation` as a first-class server-tool type —
  deferred until pi-go's [`ai.ImageProvider`](../../pkg/ai/image.go) story
  stabilizes; see [multimodal-output.md](../capabilities/multimodal-output.md).
- Stateful conversations (`store: true`) — OpenRouter is stateless; the existing
  `Store: param.NewOpt(false)` matches.
- Citation/annotation surfacing in `ai.ServerToolOutput.Content` beyond the raw
  payload that OpenRouter returns. Pretty-printing search citations is a
  follow-up (see [citations.md](../capabilities/citations.md)).

## Approach

### 1. Dialect option

Add an unexported `dialect` field to `Provider` plus a public option:

```
type Dialect int

const (
    DialectOpenAI Dialect = iota
    DialectOpenRouter
)

func WithOpenRouterDialect() Option { ... }
```

`Option` is a new package-local functional-option type (since `New` currently
takes only `option.RequestOption` from the OpenAI SDK, the dialect can't ride
on that). The cleanest shape is to switch `New` to a variadic `Option` that
internally splits into SDK request options and pi-go-side knobs. Keep the
existing call shape compatible by making bare `option.RequestOption` values
also accepted (e.g. via a small wrapper option).

The dialect is read at construction time and stored on `Provider`. All
downstream branches (`buildParams`, `convertTools`, the SSE `switch` in
`StreamText`) consult it.

### 2. Server-tool naming via raw tool params

`convertServerTool` today returns typed params (`OfWebSearchPreview`,
`OfCodeInterpreter`). For OpenRouter we need to emit a generic
`{"type": "openrouter:web_search", ...}` blob. The OpenAI Go SDK's
`responses.ToolUnionParam` is a discriminated union; if it has no raw
escape-hatch, we drop down to a custom JSON marshaller for the dialect path.

Concretely, branch in `convertTools(tools []ai.ToolInfo, d Dialect)`:

| `ai.ServerToolType` | OpenAI dialect | OpenRouter dialect |
|---|---|---|
| `ServerToolWebSearch` | `web_search_preview` (typed) | `openrouter:web_search` |
| `ServerToolWebFetch` | _(unsupported, skip)_ | `openrouter:web_fetch` |
| `ServerToolCodeExecution` | `code_interpreter` (typed) | _(unsupported on OpenRouter today, skip)_ |
| `ServerToolFileSearch` | `file_search` (typed) | _(unsupported, skip)_ |
| `ServerToolComputer` | _(not yet wired)_ | _(unsupported, skip)_ |
| `ServerToolMCP` | _(not yet wired)_ | _(unsupported, skip)_ |
| `ServerToolDateTime` (new canonical constant in [pkg/ai/tool.go](../../pkg/ai/tool.go)) | _(not native)_ | `openrouter:datetime` |

`ServerConfig` keys for OpenRouter tools follow OpenRouter's published params:
`engine`, `max_results`, `max_total_results`, `search_context_size`,
`user_location`, `allowed_domains`, `excluded_domains` (web_search);
`max_uses`, `max_content_tokens`, `allowed_domains`, `blocked_domains`
(web_fetch); `timezone` (datetime). Unknown keys are passed through verbatim.

### 3. SSE event translation

Augment the `switch event.Type` in `StreamText` so that, when
`p.dialect == DialectOpenRouter`, additional cases handle:

- `response.content_part.added` — open a text content part if not already in
  one (mirrors what `response.output_item.added` does for `message` items in
  the OpenAI path).
- `response.content_part.delta` — treat exactly like
  `response.output_text.delta` (append to `textAccum`, emit
  `EventTextDelta`).
- `response.content_part.done` — close the text part, emit `EventTextEnd`.
- `response.output_item.added` with `type` starting `"openrouter:"` — emit
  `EventToolStart` with `Server = true` and a `ServerType` derived by stripping
  the prefix (table mirrors point 2 above).
- `response.output_item.done` for `"openrouter:*"` items — assemble an
  `ai.ToolCall` with `Output.Raw` set to the raw item JSON.

Function-tool argument streaming (`response.function_call_arguments.delta` /
`.done`) is unchanged — OpenRouter is documented to support these.

The OpenAI-only events (`response.output_text.delta`,
`response.web_search_call.*`, `response.code_interpreter_call.*`,
`response.reasoning_summary_text.delta`) remain handled in the existing
branches and simply never fire under the OpenRouter dialect.

### 4. `Provider.API()` and registry dispatch

`Provider.API()` returns `"openai-responses"` for both dialects — the dialect
is a transport-level detail, not a separate API surface. Two `Provider`
instances may share the API ID; pi-go's global registry is keyed by API ID,
so callers must bind a specific provider per agent via `agent.WithProvider(p)`
rather than relying on `ai.GetProvider(model.API)`. This matches how
littlework already wires per-agent providers in
[`pkg/providers/factory.go`](../../../littlework/pkg/providers/factory.go).
Document the constraint clearly in package godoc on `Provider` and on
`WithOpenRouterDialect`.

### 5. New canonical server-tool constant

Add `ServerToolDateTime ServerToolType = "datetime"` to
[`pkg/ai/tool.go`](../../pkg/ai/tool.go) alongside the existing constants.
Keep the canonical list provider-agnostic — adapters that don't support it
silently drop, exactly like every other server-tool type today. Image
generation does **not** get a constant in this plan (deferred to the
`ai.ImageProvider` work).

## Test plan (TDD-first)

Strict TDD per [CLAUDE.md](../../CLAUDE.md). Sequence:

1. **Capture cassettes** with `internal/httprr` against the live OpenRouter
   API for: a simple text turn, a function-tool turn, an
   `openrouter:web_search` turn, an `openrouter:web_fetch` turn, an
   `openrouter:datetime` turn. Drive these via a one-off recording test
   gated behind `OPENROUTER_API_KEY`. Scrub `Authorization` like
   [openairesponses_test.go](../../pkg/ai/provider/openairesponses/openairesponses_test.go)
   already does.
2. **Write failing tests** in
   [openairesponses_test.go](../../pkg/ai/provider/openairesponses/openairesponses_test.go)
   that load each cassette through a `New(WithOpenRouterDialect(), …)`
   provider and assert the resulting `ai.Event` stream and final `ai.Message`
   match expectations.
3. **Convert/decode unit tests** in
   [convert_test.go](../../pkg/ai/provider/openairesponses/convert_test.go) —
   table-driven on `(Dialect, ToolInfo) → marshaled JSON`. Each row asserts
   the wire shape exactly.
4. **Implement** the dialect — option type, `convertTools` branch, SSE switch
   additions. Run tests until green.
5. **Capability matrix update** — table in
   [docs/capabilities/server-tools.md](../capabilities/server-tools.md) gains
   an "OpenRouter" column reflecting reality.
6. **Concept doc** — short page at
   [docs/concepts/ai/openrouter-dialect.md](../concepts/ai/openrouter-dialect.md)
   explaining the option, what tools it supports, and the SSE event
   differences. Style follows existing concept docs (frontmatter + diagram
   + 1–3 paragraphs).

## Critical files to modify

- [pkg/ai/provider/openairesponses/openairesponses.go](../../pkg/ai/provider/openairesponses/openairesponses.go) — `Provider` struct, `New`, SSE switch.
- [pkg/ai/provider/openairesponses/convert.go](../../pkg/ai/provider/openairesponses/convert.go) — `convertTools`, new `convertOpenRouterServerTool`.
- [pkg/ai/provider/openairesponses/convert_test.go](../../pkg/ai/provider/openairesponses/convert_test.go) — dialect table-driven tests.
- [pkg/ai/provider/openairesponses/openairesponses_test.go](../../pkg/ai/provider/openairesponses/openairesponses_test.go) — cassette-backed end-to-end tests.
- [pkg/ai/provider/openairesponses/testdata/](../../pkg/ai/provider/openairesponses/testdata/) — new cassettes, one per scenario.
- [pkg/ai/tool.go](../../pkg/ai/tool.go) — add `ServerToolDateTime` constant.
- [docs/capabilities/server-tools.md](../capabilities/server-tools.md) — add OpenRouter column to the matrix.
- [docs/concepts/ai/openrouter-dialect.md](../concepts/ai/openrouter-dialect.md) — new concept doc.

## Verification

- `make test` passes; `make check` clean.
- New cassette tests demonstrate end-to-end behavior on real recorded traffic
  for each tool.
- Manual smoke: register a `New(WithOpenRouterDialect(), WithBaseURL(...),
  WithAPIKey(...))` provider in a small CLI test (`cmd/pi`) with the four
  OpenRouter tools attached and a prompt that exercises each. Stream output
  to stdout, confirm citations / fetched content / datetime / image URL
  surface in the trailing `ai.Message`.

## Decisions locked in

- **API ID.** `Provider.API()` stays `"openai-responses"` for both dialects;
  callers bind per-agent via `agent.WithProvider(p)`.
- **Image generation.** Deferred. No constant, no wiring.
- **`ServerToolDateTime`.** Added as a canonical constant in
  [pkg/ai/tool.go](../../pkg/ai/tool.go).
- **Cassette recording.** Use `OPENROUTER_API_KEY` from the existing dev
  environment. Default fixture model: `openai/gpt-4o-mini` (cheap, broadly
  supported on OpenRouter Responses, exercises function-calling well). Add a
  second cassette set against `anthropic/claude-haiku-4-5` to confirm the
  dialect works for non-OpenAI underlying models — this is the whole point of
  routing through OpenRouter.
