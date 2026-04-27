---
title: "Structured Outputs"
summary: "JSON mode, JSON schema enforcement, typed responses"
read_when:
  - Forcing the model to return JSON / a specific schema
  - Implementing GenerateObject for a new provider
---

# Structured Outputs

pi-go exposes structured output through the optional [`ObjectProvider`](../../pkg/ai/object.go) interface. The generic helper [`ai.GenerateObject[T]`](../../pkg/ai/object.go) returns a typed Go value.

## Compatibility

| Provider         | API                                                   | pi-go | Notes                                                                                                                                                                    |
| ---------------- | ----------------------------------------------------- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Anthropic        | ✅ JSON mode + native schema                          | ⚠️    | implemented via _synthetic-tool trick_ — defines a tool from the schema and forces tool call ([anthropic.go:407-523](../../pkg/ai/provider/anthropic/anthropic.go#L407)) |
| OpenAI Chat      | ✅ `response_format: json_schema` with `strict: true` | ❌    | `ObjectProvider` not implemented                                                                                                                                         |
| OpenAI Responses | ✅ `text.format: json_schema` strict                  | ❌    | not implemented                                                                                                                                                          |
| Google Gemini    | ✅ `responseSchema` (subset of JSON Schema)           | ❌    | not implemented                                                                                                                                                          |
| Claude CLI       | ✅ `--json-schema` flag                               | ✅    | ([claude.go:194-250](../../pkg/ai/provider/claudecli/claude.go#L194))                                                                                                    |
| Gemini CLI       | ✅ via `responseSchema`                               | ❌    |                                                                                                                                                                          |

## Provider Documentation

- [Anthropic — JSON output](https://docs.anthropic.com/en/docs/test-and-evaluate/strengthen-guardrails/increase-consistency)
- [OpenAI — Structured outputs](https://platform.openai.com/docs/guides/structured-outputs)
- [Google Gemini — Structured output](https://ai.google.dev/gemini-api/docs/structured-output)

## pi-go Gaps

- **OpenAI Chat / Responses** native `response_format` not wired, even though both APIs offer guaranteed strict-schema output.
- **Google** `responseSchema` not wired.
- **Anthropic synthetic-tool path** works but bypasses native JSON support — emits an extra tool round-trip and forfeits any model-specific JSON-mode optimizations.
- **`OutputSchema` on `ai.ToolInfo`** is defined but unused — could drive native structured output for tool results.
