---
title: "Messages"
summary: "Message struct, roles, constructors, stop reasons, and the Prompt type"
read_when:
  - Building or manipulating conversation history
  - Working with message roles or content blocks
---

# Messages

A `Message` represents a single message in a conversation. Messages carry content blocks, role metadata, and role-specific fields. They are the primary data structure flowing through the SDK.

## Roles

Three roles: `user`, `assistant`, `tool_result`. Role-specific fields are zero-valued when not applicable — assistant messages carry provider metadata and token usage; tool result messages carry the call ID and tool name they respond to.

## Design: flat struct with role-specific fields

We use a single `Message` struct rather than separate types per role. This keeps slices homogeneous (`[]Message`), simplifies serialization, and matches the wire format of most provider APIs. The tradeoff is that some fields are meaningless for certain roles — but Go's zero values handle this cleanly.

## Constructors

Helper functions (`UserMessage`, `AssistantMessage`, `ToolResultMessage`, etc.) set the correct role and timestamp. Always prefer these over constructing `Message` literals directly.

## Stop reasons

`StopReason` indicates why generation stopped: natural completion (`stop`), token limit (`length`), tool use (`tool_use`), error, or context cancellation (`aborted`). The agent loop uses `tool_use` to decide whether to continue with another turn.

## Prompt

`Prompt` bundles the inputs for a model call: system prompt text, conversation history, and available tool definitions. It maps 1:1 to a provider API call. The agent builds this internally; direct callers of `StreamText`/`GenerateText` construct it themselves.

## JSON serialization

Custom `MarshalJSON`/`UnmarshalJSON` for clean wire format — content blocks carry a `type` discriminator, zero-valued fields are omitted, timestamps use RFC3339Nano. Server-tool calls round-trip the `Server`, `ServerType`, and `Output` (`Content`, `Raw`, `IsError`) fields on `ToolCall` so persisted history replays with the same shape providers produced.

## Related

- [Content](/concepts/ai/content) — content block types
- [Usage](/concepts/ai/usage) — token usage carried on assistant messages
- [Tools](/concepts/ai/tools) — tool calling and results
