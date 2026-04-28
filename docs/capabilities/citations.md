---
title: "Citations"
summary: "Inline source attribution for generated text"
read_when:
  - Building features that need verifiable sources
  - Surfacing search-grounded results
---

# Citations

Citations attach source metadata (document, page, character range, URL) to spans of generated text. Useful for RAG over uploaded files, web-search grounding, and audit trails.

## Compatibility

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ Citations API (`citations: {enabled: true}` on documents) | ⚠️ | document-level Citations API not wired; `web_search` results surface as a numbered title/URL list on `ServerToolOutput` ([anthropic.go](../../pkg/ai/provider/anthropic/anthropic.go), see [server-tools.md](server-tools.md)) |
| OpenAI Chat | ❌ | — | |
| OpenAI Responses | ✅ inline citations from web/file search | ⚠️ | web-search action description on `ServerToolOutput.Content`; full provider JSON retained on `Output.Raw` ([openairesponses.go](../../pkg/ai/provider/openairesponses/openairesponses.go)). No dedicated citation content type yet |
| Google Gemini | ✅ `groundingMetadata` from grounded searches | ⚠️ | parsed into a synthesized `web_search` `ToolCall`; chunks rendered on `Output.Content`, full metadata on `Output.Raw` ([google.go](../../pkg/ai/provider/google/google.go)) |
| Claude CLI | ⚠️ surfaced in CLI output | ❌ | not parsed |
| Gemini CLI | ❌ | — | |

## Provider Documentation

- [Anthropic — Citations](https://docs.anthropic.com/en/docs/build-with-claude/citations)
- [Google Gemini — Grounding metadata](https://ai.google.dev/gemini-api/docs/google-search#grounded-responses)

## pi-go Gaps

- No `Citation` content type or sub-block on `ai.Text`. Search-grounded citations currently live as text + raw JSON inside `ServerToolOutput`, not as structured spans tied to assistant text.
- No way to opt-in citations on document inputs (Anthropic's `citations: {enabled}` flag).
- No anchoring of citation chunks to the assistant text spans they support — Gemini's `groundingSupports` indices are present in `Output.Raw` but not promoted to a typed structure.
- Citations interact with [Server-Side Tools](server-tools.md) and [File Search](files-api.md) — the `ServerToolOutput.Raw` escape hatch is shared across all of them.
