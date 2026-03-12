---
title: "Content"
summary: "Content block types: Text, Thinking, Image, ToolCall"
read_when:
  - Working with message content blocks
  - Handling thinking/reasoning output or tool calls
---

# Content

Content blocks are the building blocks inside [Messages](/concepts/ai/messages). Each message contains a slice of `Content`.

## Design: sealed interface

`Content` is a sealed interface (unexported `content()` method). External packages cannot add implementations. This ensures exhaustive handling — if you match all four types (`Text`, `Thinking`, `Image`, `ToolCall`), you've covered everything. New content types require an SDK release.

Use `AsContent[T]` for safe type assertion — it handles nil safely.

## Content types

- **Text** — plain text content. May carry a provider-specific `Signature` for integrity verification (OpenAI, Google).
- **Thinking** — reasoning/chain-of-thought from models that support extended thinking. Only present when thinking is enabled via `WithThinking`. See [Options](/concepts/ai/options).
- **Image** — base64-encoded image data with MIME type. Used in user messages (input images) and image generation results.
- **ToolCall** — a tool invocation from the model. Carries `ID`, `Name`, and `Arguments`. The agent loop matches `Name` to a registered [Tool](/concepts/ai/tools), executes it, and feeds back a `ToolResultMessage`. `ID` links the call to its result.

## Related

- [Messages](/concepts/ai/messages) — messages contain content blocks
- [Tools](/concepts/ai/tools) — `ToolCall` triggers tool execution
- [Options](/concepts/ai/options) — `WithThinking` enables `Thinking` content
