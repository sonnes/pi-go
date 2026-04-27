---
title: "Realtime / Live API"
summary: "Bidirectional realtime audio + video streaming"
read_when:
  - Building voice agents
  - Building low-latency multimodal interfaces
---

# Realtime / Live API

## Compatibility

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ❌ | — | |
| OpenAI | ✅ Realtime API (WebRTC / WebSocket); voice + tools | ❌ | not wired |
| Google Gemini | ✅ Live API; 24-language voice; multimodal realtime | ❌ | not wired |
| Claude CLI | ❌ | — | |
| Gemini CLI | ❌ | — | |

## Provider Documentation

- [OpenAI — Realtime API](https://platform.openai.com/docs/guides/realtime)
- [Google Gemini — Live API](https://ai.google.dev/gemini-api/docs/live)

## pi-go Gaps

- No bidirectional streaming primitive — `EventStream` is one-way.
- No audio I/O. See [Audio Input](audio-input.md) and [Audio Output](audio-output.md).
- WebSocket / WebRTC transports not present anywhere in the codebase.
- This is a fundamentally different shape from `StreamText`; would warrant a separate `RealtimeProvider` interface.
