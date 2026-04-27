---
title: "Multimodal Input"
summary: "Sending images, documents, audio, and video as user content"
read_when:
  - Building a feature that accepts non-text user content
  - Wiring a new input modality on a provider adapter
---

# Multimodal Input

[`ai.Content`](../../pkg/ai/content.go) defines the input variants pi-go can carry. Today: `Text`, `Image` (base64 + MIME), `File` (Data/URL/FileID + filename + MIME). No audio or video variants exist.

## Images

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ jpeg/png/gif/webp; base64 or URL | ⚠️ base64 only | `NewImageBlockBase64` ([anthropic.go:71](../../pkg/ai/provider/anthropic/anthropic.go#L71)) |
| OpenAI Chat | ✅ base64 or URL with `detail` | ⚠️ base64 only as data: URL | ([convert.go:74-85](../../pkg/ai/provider/openai/convert.go#L74)) |
| OpenAI Responses | ✅ base64 or URL with `detail` | ⚠️ base64 only | `detail: auto` ([convert.go:66-80](../../pkg/ai/provider/openairesponses/convert.go#L66)) |
| Google Gemini | ✅ InlineData base64; Files URI; `media_resolution` per image | ✅ base64; URL via `File` block | ([convert.go:66-71](../../pkg/ai/provider/google/convert.go#L66)) |
| Claude CLI | ✅ via `@filepath` | ❌ | image content silently dropped |
| Gemini CLI | ✅ InlineData | ✅ | ([convert.go:64-69](../../pkg/ai/provider/geminicli/convert.go#L64)) |

**Provider docs**

- [Anthropic — Vision](https://docs.anthropic.com/en/docs/build-with-claude/vision)
- [OpenAI — Vision](https://platform.openai.com/docs/guides/vision)
- [Google Gemini — Image understanding](https://ai.google.dev/gemini-api/docs/image-understanding)

## Documents (PDF, plain text, DOCX, CSV, XLSX, MD)

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ PDF (URL/base64), plain text via DocumentBlock; DOCX/CSV/XLSX/MD via Files API | ⚠️ PDF + plain text only; FileID-only files dropped | `NewDocumentBlock` ([convert.go:88-105](../../pkg/ai/provider/anthropic/convert.go#L88)); plain text base64-decoded into `PlainTextSourceParam` ([convert.go:92-101](../../pkg/ai/provider/anthropic/convert.go#L92)) |
| OpenAI Chat | ✅ FileID or inline base64 | ✅ FileID + base64; URL form skipped | ([convert.go:107-133](../../pkg/ai/provider/openai/convert.go#L107)) |
| OpenAI Responses | ✅ FileID, URL, or base64 | ✅ all three | ([convert.go:102-129](../../pkg/ai/provider/openairesponses/convert.go#L102)) |
| Google Gemini | ✅ Files URI/FileID, or base64 InlineData | ✅ all three | ([convert.go:86-117](../../pkg/ai/provider/google/convert.go#L86)) |
| Claude CLI | ⚠️ via filepath arg | ❌ | not forwarded |
| Gemini CLI | ✅ all three | ✅ | ([convert.go:83-112](../../pkg/ai/provider/geminicli/convert.go#L83)) |

**Provider docs**

- [Anthropic — PDF support](https://docs.anthropic.com/en/docs/build-with-claude/pdf-support)
- [OpenAI — File inputs](https://platform.openai.com/docs/guides/pdf-files)
- [Google Gemini — Document understanding](https://ai.google.dev/gemini-api/docs/document-processing)

## Audio

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ❌ | ❌ | not supported by API |
| OpenAI Chat | ✅ `gpt-4o-audio-preview` `input_audio` | ❌ | no Go content type |
| OpenAI Responses | ✅ | ❌ | |
| Google Gemini | ✅ WAV, MP3, AIFF, AAC, OGG, FLAC | ❌ | API supports inline + Files API |
| Gemini Live | ✅ realtime audio | ❌ | see [realtime-api.md](realtime-api.md) |
| Claude CLI | ❌ | — | |
| Gemini CLI | ⚠️ via Files API ref | ❌ | |

**Provider docs**

- [OpenAI — Audio inputs](https://platform.openai.com/docs/guides/audio)
- [Google Gemini — Audio understanding](https://ai.google.dev/gemini-api/docs/audio)

## Video

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ❌ | ❌ | |
| OpenAI Chat | ⚠️ via frame sampling | ❌ | passes frames as images |
| OpenAI Responses | ✅ | ❌ | |
| Google Gemini | ✅ inline or Files API; up to 1h; `videoMetadata` for offsets/FPS | ❌ | strongest video support |
| Claude CLI | ❌ | — | |
| Gemini CLI | ⚠️ via Files API ref | ❌ | |

**Provider docs**

- [Google Gemini — Video understanding](https://ai.google.dev/gemini-api/docs/video-understanding)

## pi-go Gaps

- **URL images** not used by any provider; `ai.Image` only carries `Data`. Anthropic, OpenAI, and Gemini all accept URL form natively.
- **Image detail / media_resolution** hint is hard-coded to `auto`; not exposed.
- **Anthropic FileID-only documents** are silently dropped ([convert.go:86-108](../../pkg/ai/provider/anthropic/convert.go#L86)).
- **OpenAI Chat URL-only files** silently dropped ([convert.go:108-110](../../pkg/ai/provider/openai/convert.go#L108)).
- **No `Audio` content variant**, no audio conversion code anywhere.
- **No `Video` content variant**; Gemini's `videoMetadata` (start/end offset, FPS) cannot be expressed.
- **Claude CLI** drops all non-text content.
- **Files API uploads** missing — see [files-api.md](files-api.md).
