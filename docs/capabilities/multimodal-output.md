---
title: "Multimodal Output"
summary: "Generating images and audio (TTS) from text prompts"
read_when:
  - Adding image generation or TTS to a feature
  - Implementing the ImageProvider interface for a new provider
---

# Multimodal Output

pi-go exposes image generation through the optional [`ImageProvider`](../../pkg/ai/image.go) capability interface. The entry point `catalog.GenerateImage` (also a package-level helper in `pi`) resolves a `"<provider>/<model>"` spec and takes a `Prompt`, mirroring `GenerateText`; set dimensions and image count with `WithImageSize` / `WithImageCount`. TTS has the same shape via [`SpeechProvider`](../../pkg/ai/speech.go) and `GenerateSpeech`, though no built-in provider implements it yet.

## Image Generation

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ❌ (only via MCP integrations) | ❌ | |
| OpenAI Chat | ✅ separate Images API (`dall-e-3`, `gpt-image-1`) | ✅ | DALL·E only; default `dall-e-3` |
| OpenAI Responses | ✅ inline `image_generation` server tool | ❌ | not exposed as in-stream tool |
| Google Gemini | ✅ separate Imagen API (`imagen-3.0-generate-002`, Imagen 4 series) | ⚠️ | Imagen 3.0 only |
| Claude CLI | ❌ | — | |
| Gemini CLI | ❌ | — | |

**Provider docs**

- [OpenAI — Image generation](https://platform.openai.com/docs/guides/images)
- [Google — Imagen on Gemini API](https://ai.google.dev/gemini-api/docs/image-generation)
- [OpenAI Responses — image_generation tool](https://platform.openai.com/docs/guides/tools-image-generation)

## Audio Output (TTS)

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ❌ | ❌ | |
| OpenAI Chat | ✅ Audio API (`gpt-4o-mini-tts`, voices) | ❌ | |
| OpenAI Responses | ✅ inline TTS | ❌ | |
| Google Gemini | ✅ Gemini 3.1 Flash TTS, multi-speaker | ❌ | |
| Gemini Live | ✅ realtime audio out | ❌ | see [realtime-api.md](realtime-api.md) |
| Claude CLI | ❌ | — | |
| Gemini CLI | ❌ | — | |

**Provider docs**

- [OpenAI — Text-to-Speech](https://platform.openai.com/docs/guides/text-to-speech)
- [Google Gemini — Speech generation](https://ai.google.dev/gemini-api/docs/speech-generation)

## pi-go Gaps

- **OpenAI Responses inline `image_generation`** not wired — would let one agentic call return both text and an image alongside other tool use.
- **Imagen 4 / 5** not surfaced in the Google provider.
- **Quality / style / response_format** options on OpenAI Images are not wired — only `WithImageSize` and `WithImageCount` reach the provider.
- **No `AudioProvider` interface**, no audio output content variant, no streaming audio frame consumer.
- Anthropic image generation depends on [MCP](server-tools.md#mcp-connectors), which is also missing.
