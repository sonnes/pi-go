---
title: Capture OpenRouter Citation Annotations
date: 2026-04-28
status: todo
---

# Capture OpenRouter Citation Annotations

## Context

OpenRouter's `openrouter:web_search` server tool returns search results as
inline annotations on the model's text output rather than as structured
fields on the tool item. Each annotation arrives as a
`response.output_text.annotation.added` SSE event with a `url_citation`
payload (URL, title, span). The
[OpenRouter dialect](../concepts/ai/openrouter-dialect.md) currently
ignores these events, so the persisted `ai.Message` carries:

- A server-tool `ai.ToolCall` with empty `Arguments` and a minimal
  `Output.Raw` (`{id, type, status}` only).
- The model's text content with no source attribution.

The grounding sources end up in the chat UI by way of the model copying
URLs into prose, but downstream consumers (history, audit, citation UI)
have no structured handle on them.

## Goal

Capture `output_text.annotation.added` events during streaming and surface
them in the final `ai.Message` so consumers can:

1. Display citations alongside the assistant text (footnote markers, hover
   cards, "Sources" panel).
2. Tie each citation back to the preceding `openrouter:web_search` call
   that produced it, so a server-tool call's `Output` shows what came back
   from the search and not just `{id, type, status}`.

## Non-goals

- A general-purpose `ai.Citation` content type spanning every provider.
  Anthropic, Google, and OpenAI Responses each ship a different shape; a
  unified type is its own design problem
  ([citations.md gaps](../capabilities/citations.md)).
- Surfacing citations through the SSE `ai.Event` stream (not the final
  message). Streaming consumers can re-derive from the message if needed.

## Approach (sketch)

1. **Inspect the event payload** — record a `web_search`-heavy cassette
   and document the exact JSON shape of `output_text.annotation.added` /
   `url_citation`. The
   [TestOpenRouter_ServerWebSearch.httprr](../../pkg/ai/provider/openairesponses/testdata/TestOpenRouter_ServerWebSearch.httprr)
   cassette already contains samples; pull a representative one into a
   fixture under `testdata/`.

2. **Accumulate during streaming** — in
   [openairesponses.go](../../pkg/ai/provider/openairesponses/openairesponses.go)'s
   SSE switch, add a case for `response.output_text.annotation.added`.
   Append each annotation (URL, title, character span) to a per-stream
   slice scoped by the current text content index.

3. **Attach to the server-tool call's Output** — when the corresponding
   `openrouter:web_search` `output_item.done` arrives, fold the
   accumulated annotations into `ServerToolOutput.Raw` (extending the
   bare `{id, type, status}` shape with a `citations: [...]` array) and
   render `Output.Content` as a numbered list of `[N] Title — URL` lines
   so persisted-message readers see a useful summary.

4. **Persist round-trip** — extend
   [message_test.go](../../pkg/ai/message_test.go)'s
   `TestMessageJSONRoundTrip_ServerToolCall` to include a populated
   citations array in the raw payload; verify it survives
   marshal/unmarshal unchanged via the existing `Output.Raw` field (no
   schema change needed today).

## Open questions

- How are annotations associated with a specific server-tool call when
  multiple `openrouter:web_search` calls fire in one turn? Likely by
  output_index proximity, but worth verifying against a multi-call
  cassette.
- Should `Output.Content` summarize the search _query_ (which we don't
  see today) or the _results_ (URLs/titles)? Probably results — the
  query is implicit and rarely useful for display.

## Critical files (when picked up)

- [pkg/ai/provider/openairesponses/openairesponses.go](../../pkg/ai/provider/openairesponses/openairesponses.go)
- [pkg/ai/provider/openairesponses/openairesponses_test.go](../../pkg/ai/provider/openairesponses/openairesponses_test.go)
  (or a new `citations_test.go`)
- [pkg/ai/provider/openairesponses/testdata/](../../pkg/ai/provider/openairesponses/testdata/)
- [docs/capabilities/citations.md](../capabilities/citations.md) — flip
  the OpenRouter row from ❌ to ⚠️ once annotations are captured but not
  yet bound to text spans.
