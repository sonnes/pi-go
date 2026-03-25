---
title: "Agent Messages"
summary: "Extensible message type wrapping ai.Message with support for custom application messages and roles"
read_when:
  - Working with agent conversation history
  - Adding custom message types (artifacts, UI state, notifications)
  - Filtering messages before LLM calls
---

# Agent Messages

The agent package defines its own `Message` interface that wraps `ai.Message` and supports application-defined custom messages. Custom messages flow through the conversation history but are filtered out before LLM calls.

## Roles

Every `Message` exposes a `Role()` method returning an agent-level `Role`. Four roles are predefined: `user`, `assistant`, `tool_result`, `system`. Applications can define additional roles — `Role` is an open `string` type, not a closed enum.

`LLMMessage` derives its role from the underlying `ai.Role`. `CustomMessage` carries an explicit `CustomRole` field set at construction.

## Design: sealed interface with embedding

`Message` follows the same sealed-interface pattern as `ai.Content` — an unexported marker method prevents arbitrary implementations. Two concrete types satisfy it:

- **`LLMMessage`** — wraps an `ai.Message`. These are the messages that reach the model.
- **`CustomMessage`** — base type that applications embed to define their own message types. Carries a `CustomRole` and `Kind` field.

External packages extend the system by embedding `CustomMessage`, not by implementing the marker directly. This keeps the set of "shapes" open for applications while preserving exhaustive type-switching over the two categories (LLM vs custom).

## Why not use `ai.Message` directly?

The `ai.Message` type maps 1:1 to provider wire formats. It has no room for application concerns — UI artifacts, status indicators, agent-internal metadata. Wrapping it in an interface lets the conversation history carry both LLM messages and application messages in a single `[]Message` slice, without polluting the LLM-level type.

## Custom messages

Applications define custom messages by embedding `CustomMessage` and adding their own fields. `CustomRole` sets the message's role; `Kind` acts as an application-level discriminator:

```go
type ArtifactMessage struct {
    agent.CustomMessage
    Title   string
    Content string
}

msg := ArtifactMessage{
    CustomMessage: agent.CustomMessage{
        CustomRole: agent.RoleUser,
        Kind:       "artifact",
    },
    Title:   "Generated Code",
    Content: "func main() { ... }",
}
```

System-level instructions use the `system` role:

```go
instruction := agent.CustomMessage{
    CustomRole: agent.RoleSystem,
    Kind:       "instruction",
}
```

Custom messages participate in the conversation history, are visible via `Messages()`, and survive across turns. They are invisible to the model — `LLMMessages` filters them out.

## Filtering

`LLMMessages([]Message) []ai.Message` extracts only the `LLMMessage` values, returning their underlying `ai.Message` values. By default, the agent loop calls this before each model invocation.

The `TransformMessages` hook (set via `WithHooks`) replaces this default conversion, giving extensions full control over what the model sees. The hook receives the full `[]Message` — including custom messages — and returns `[]ai.Message`. This enables use cases like converting custom messages into LLM-visible context, pruning old messages, or injecting synthetic messages.

The agent exposes both views:
- `Messages()` on the `Agent` interface — full history including custom messages.
- `LLMMessages([]Message)` utility function — LLM-only view for inspection or debugging.

## Type switching

Use standard Go type switches to handle messages:

```go
for _, m := range agent.Messages() {
    switch v := m.(type) {
    case agent.LLMMessage:
        // v.Message is an ai.Message
    case ArtifactMessage:
        // application-defined type
    }
}
```

`AsLLMMessage` is a convenience for the common single-type assertion.

## Related

- [ai.Message](/concepts/ai/messages) — the LLM-level message type that `LLMMessage` wraps
- [ai.Content](/concepts/ai/content) — uses the same sealed-interface pattern
- [Agent State](/concepts/agent/agent-state) — runtime state observability
