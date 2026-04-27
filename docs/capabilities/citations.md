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
| Anthropic | ✅ Citations API (`citations: {enabled: true}` on documents) | ❌ | not wired |
| OpenAI Chat | ❌ | — | |
| OpenAI Responses | ✅ inline citations from web/file search | ❌ | not wired |
| Google Gemini | ✅ `groundingMetadata` from grounded searches | ❌ | not wired |
| Claude CLI | ⚠️ surfaced in CLI output | ❌ | not parsed |
| Gemini CLI | ❌ | — | |

## Provider Documentation

- [Anthropic — Citations](https://docs.anthropic.com/en/docs/build-with-claude/citations)
- [Google Gemini — Grounding metadata](https://ai.google.dev/gemini-api/docs/google-search#grounded-responses)

## pi-go Gaps

- No `Citation` content type or sub-block on `ai.Text`.
- No way to opt-in citations on document inputs (Anthropic's `citations: {enabled}` flag).
- No `groundingMetadata` parser for Gemini responses.
- Citations interact with [Web Search](web-search.md), [Web Fetch](web-fetch.md), and [File Search](file-search.md) — gap is shared.
