---
title: "Streaming"
summary: "Server-sent event streaming of text, thinking, and tool-call deltas"
read_when:
  - Implementing or debugging a streaming provider adapter
  - Understanding which providers stream tool-call arguments
---

# Streaming

All major providers expose Server-Sent Events for incremental output. pi-go's [`Provider.StreamText`](../../pkg/ai/provider.go) returns an [`EventStream`](../../pkg/ai/event.go) that yields typed deltas (`text_delta`, `thinking_delta`, `tool_delta`).

## Compatibility

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ SSE with fine-grained tool-call streaming | ✅ | `client.Messages.NewStreaming` ([anthropic.go:100](../../pkg/ai/provider/anthropic/anthropic.go#L100)) |
| OpenAI Chat | ✅ SSE | ✅ | `Chat.Completions.NewStreaming` ([openai.go:75](../../pkg/ai/provider/openai/openai.go#L75)) |
| OpenAI Responses | ✅ event stream | ✅ | `Responses.NewStreaming` ([openairesponses.go:63](../../pkg/ai/provider/openairesponses/openairesponses.go#L63)) |
| Google Gemini | ✅ SSE | ✅ | `chat.SendMessageStream` ([google.go:157](../../pkg/ai/provider/google/google.go#L157)) |
| Claude CLI | ✅ NDJSON via `--output-format stream-json` | ⚠️ | pi-go consumes the NDJSON but emits a single completed message rather than incremental events ([claudecli/claude.go](../../pkg/ai/provider/claudecli/claude.go)) |
| Gemini CLI | ✅ SSE at `/v1internal:streamGenerateContent?alt=sse` | ✅ | direct HTTP+SSE ([geminicli.go:24](../../pkg/ai/provider/geminicli/geminicli.go#L24)) |

## Provider Documentation

- [Anthropic — Streaming Messages](https://docs.anthropic.com/en/docs/build-with-claude/streaming)
- [OpenAI — Streaming Responses](https://platform.openai.com/docs/api-reference/streaming)
- [OpenAI Responses — Event types](https://platform.openai.com/docs/api-reference/responses-streaming)
- [Google Gemini — Streaming generate](https://ai.google.dev/gemini-api/docs/text-generation#stream)
- [Claude Code CLI — `--output-format stream-json`](https://docs.claude.com/en/docs/claude-code/cli-reference)

## pi-go Gaps

- **Claude CLI** does not emit per-delta events; full message is returned once. Streaming-style consumers see no progressive output.
- No common heartbeat / keep-alive handling — long-running reasoning calls rely on the underlying SDK to keep the connection alive.
