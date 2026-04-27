---
title: "Stateful Conversations"
summary: "Provider-managed conversation state via stored sessions"
read_when:
  - Building long-running agents
  - Reducing per-turn payload size
---

# Stateful Conversations

OpenAI's Responses API and Google's Interactions API can persist conversation state server-side; the client passes a `previous_response_id` (OpenAI) or session reference rather than re-sending the entire history each turn.

## Compatibility

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ❌ stateless (history is client-managed) | — | |
| OpenAI Chat | ❌ stateless | — | |
| OpenAI Responses | ✅ Conversations API; `store: true`, `previous_response_id` | ❌ | pi-go forces `store: false` ([openairesponses.go:303](../../pkg/ai/provider/openairesponses/openairesponses.go#L303)) |
| Google Gemini | ✅ Interactions API | ❌ | not wired |
| Claude CLI | ✅ session JSONL in `~/.claude/projects/` | ❌ | each pi-go call spawns a fresh subprocess ([claude.go:5-8](../../pkg/ai/provider/claudecli/claude.go#L5)); only last user message replayed |
| Gemini CLI | ✅ session managed by CLI | ❌ | |

## Provider Documentation

- [OpenAI — Conversations API](https://platform.openai.com/docs/api-reference/conversations)
- [OpenAI — `previous_response_id`](https://platform.openai.com/docs/guides/migrate-to-responses)

## pi-go Gaps

- pi-go's design replays full message history each turn — works everywhere but forfeits cost/latency wins from server-side state.
- No `SessionStore` abstraction.
- Claude CLI's `--resume` / `--session-id` not wired, so multi-turn agents lose conversation history between calls.
