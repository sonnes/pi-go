---
title: "Content"
summary: "Content block types: Text, Thinking, Image, File, ToolCall"
read_when:
  - Working with message content blocks
  - Handling thinking/reasoning output or tool calls
  - Attaching documents/files to user messages
---

# Content

Content blocks are the building blocks inside [Messages](/concepts/ai/messages). Each message contains a slice of `Content`.

## Design: sealed interface

`Content` is a sealed interface (unexported `content()` method). External packages cannot add implementations. This ensures exhaustive handling — if you match all five types (`Text`, `Thinking`, `Image`, `File`, `ToolCall`), you've covered everything. New content types require an SDK release.

Use `AsContent[T]` for safe type assertion — it handles nil safely.

## Content types

- **Text** — plain text content. May carry a provider-specific `Signature` for integrity verification (OpenAI, Google).
- **Thinking** — reasoning/chain-of-thought from models that support extended thinking. Only present when thinking is enabled via `WithThinking`. See [Options](/concepts/ai/options).
- **Image** — base64-encoded image data with MIME type. Used in user messages (input images) and image generation results.
- **File** — a document/file attachment in user messages. Exactly one of `Data` (base64), `URL`, or `FileID` should be set. `MimeType` is the IANA media type (e.g. `application/pdf`, `text/plain`); `Filename` is optional.
- **ToolCall** — a tool invocation from the model. Carries `ID`, `Name`, and `Arguments`. The agent loop matches `Name` to a registered [Tool](/concepts/ai/tools), executes it, and feeds back a `ToolResultMessage`. `ID` links the call to its result.
  - For provider-hosted **server tools** (web search, code execution, ...), the same `ToolCall` block carries the result inline: `Server` is `true`, `ServerType` identifies the canonical tool, and `Output` is a `*ServerToolOutput`. There is no separate `ToolResultMessage` — the provider has already executed the call and the agent skips local execution.

## ServerToolOutput

`ServerToolOutput` is the result of a provider-executed server tool, attached to its `ToolCall`:

- **`Content`** — a normalized text rendering of the result (e.g. a numbered list of "Title — URL" entries for web search, concatenated stdout/stderr for code execution). Suitable for display or for feeding back into a prompt.
- **`Raw`** — the provider's original JSON, retained so callers can extract structured fields (citations, encrypted indices, per-chunk confidence scores) that don't fit the normalized rendering.
- **`IsError`** — true when the provider reported a failed invocation.

## File support per provider

`File` blocks are input-only (user messages). Provider support varies by source variant:

| Provider | Inline `Data` | `URL` | `FileID` | Notes |
|---|---|---|---|---|
| Anthropic | PDF, plain text | PDF | — | Maps to `document` block; FileIDs are not supported. |
| OpenAI Chat Completions | yes | — | yes | `URL` is dropped; use the Files API to obtain a `FileID`. |
| OpenAI Responses | yes | yes | yes | Maps to `input_file`. |
| Google Gemini | yes | yes | yes | Inline uses `inlineData`; URL/FileID use `fileData`. |
| Gemini CLI | yes | yes | yes | Same shape as Google. |
| Claude CLI | — | — | — | The CLI subprocess only forwards user text; files are skipped. |

Unsupported variants are silently skipped at the provider boundary so messages with mixed-support content can still flow through.

## Related

- [Messages](/concepts/ai/messages) — messages contain content blocks
- [Tools](/concepts/ai/tools) — `ToolCall` triggers tool execution
- [Options](/concepts/ai/options) — `WithThinking` enables `Thinking` content
