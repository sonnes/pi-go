---
title: "Structured Outputs"
summary: "JSON mode, JSON schema enforcement, typed responses"
read_when:
  - Forcing the model to return JSON / a specific schema
  - Implementing GenerateObject for a new provider
---

# Structured Outputs

pi-go exposes structured output through the optional [`ObjectProvider`](../../pkg/ai/object.go) interface. The generic helper [`ai.GenerateObject[T]`](../../pkg/ai/generate.go) resolves a `"<provider>/<model>"` spec and returns a typed Go value, mirroring [`ai.Generate`](../../pkg/ai/generate.go).

## Compatibility

| Provider         | API                                                   | pi-go | Notes                                                                                                                                                                    |
| ---------------- | ----------------------------------------------------- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Anthropic        | ✅ JSON mode + native schema                          | ⚠️    | implemented via _synthetic-tool trick_ — defines a tool from the schema and forces tool call ([anthropic.go:407-523](../../pkg/ai/provider/anthropic/anthropic.go#L407)) |
| OpenAI Chat      | ✅ `response_format: json_schema` with `strict: true` | ⚠️    | implemented via JSON object mode (`response_format: json_object`); the derived schema is **not** sent, so output is valid JSON but not schema-constrained ([openai.go:414](../../pkg/ai/provider/openai/openai.go#L414)) |
| OpenAI Responses | ✅ `text.format: json_schema` strict                  | ❌    | not implemented                                                                                                                                                          |
| Google Gemini    | ✅ `responseSchema` (subset of JSON Schema)           | ✅    | native `responseSchema` via `ResponseJsonSchema` ([google.go:700](../../pkg/ai/provider/google/google.go#L700))                                                          |
| Claude CLI       | ✅ `--json-schema` flag                               | ✅    | ([claude.go:194-250](../../pkg/ai/provider/claudecli/claude.go#L194))                                                                                                    |
| Codex CLI        | ✅ `--output-schema` flag                             | ✅    | writes the JSON Schema to a temp file and passes it to `codex exec --output-schema` ([codex.go](../../pkg/ai/provider/codexcli/codex.go))                                |
| Gemini CLI       | ✅ via `responseSchema`                               | ❌    |                                                                                                                                                                          |

## Provider Documentation

- [Anthropic — JSON output](https://docs.anthropic.com/en/docs/test-and-evaluate/strengthen-guardrails/increase-consistency)
- [OpenAI — Structured outputs](https://platform.openai.com/docs/guides/structured-outputs)
- [Google Gemini — Structured output](https://ai.google.dev/gemini-api/docs/structured-output)

## pi-go Gaps

- **OpenAI Chat** uses plain `json_object` mode without sending the schema; strict native `json_schema` is not wired. **OpenAI Responses** structured output is unimplemented.
- **Anthropic synthetic-tool path** works but bypasses native JSON support — emits an extra tool round-trip and forfeits any model-specific JSON-mode optimizations.
- **`OutputSchema` on `ai.ToolInfo`** is defined but unused — could drive native structured output for tool results.
