---
title: "Function Calling"
summary: "Custom (client-side) tools — definitions, parallel calls, tool choice"
read_when:
  - Defining tools for a feature
  - Wiring tool calls in a new provider adapter
---

# Function Calling

Tools are declared via [`ai.ToolInfo`](../../pkg/ai/tool.go) (name, description, JSON schema, optional `Parallel` flag). Each provider adapter converts `ToolInfo` to its native function-call format. Tool results return as [`ai.RoleToolResult`](../../pkg/ai/message.go) messages carrying `ToolCallID`, `ToolName`, and an `IsError` flag.

For provider-hosted tools (web search, code execution, computer use, etc.), see [server-tools.md](server-tools.md).

## Tool Declarations

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ `tools` array; strict-by-default | ✅ | `ToolParam` ([convert.go:169-208](../../pkg/ai/provider/anthropic/convert.go#L169)) |
| OpenAI Chat | ✅ `tools` array with strict mode | ✅ | `ChatCompletionToolParam` ([convert.go:190-214](../../pkg/ai/provider/openai/convert.go#L190)); strict gated by compat flag |
| OpenAI Responses | ✅ `tools` array | ✅ | `FunctionToolParam` ([convert.go:208-231](../../pkg/ai/provider/openairesponses/convert.go#L208)) |
| Google Gemini | ✅ `tools[].functionDeclarations` | ✅ | |
| Claude CLI | ❌ (CLI manages its own tools) | ❌ | `Tools` ignored ([claude.go:124](../../pkg/ai/provider/claudecli/claude.go#L124)) |
| Gemini CLI | ✅ functionDeclarations | ✅ | |

## Parallel Tool Calls

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ multiple `tool_use` blocks per response; `disable_parallel_tool_use` flag | ⚠️ | streaming accumulates blocks; flag not forwarded |
| OpenAI Chat | ✅ `parallel_tool_calls: true` (default) | ⚠️ | deltas accumulated by index ([openai.go:150-193](../../pkg/ai/provider/openai/openai.go#L150)); flag not exposed |
| OpenAI Responses | ✅ multiple output items | ✅ | tracked by index ([openairesponses.go:158](../../pkg/ai/provider/openairesponses/openairesponses.go#L158)) |
| Google Gemini | ✅ multiple `functionCall` parts | ⚠️ | parts iterated; no flag |
| Claude CLI | ❌ | — | |
| Gemini CLI | ✅ | ⚠️ | |

## Tool Choice

[`ai.ToolChoice`](../../pkg/ai/options.go) supports `auto` (default), `none`, `required`, or a specific tool name.

| Provider | auto | none | required / any | specific | pi-go |
|---|---|---|---|---|---|
| Anthropic | ✅ | ✅ | ✅ `any` | ✅ | ✅ all four ([convert.go:215-231](../../pkg/ai/provider/anthropic/convert.go#L215)) |
| OpenAI Chat | ✅ | ✅ | ✅ `required` | ✅ | ✅ all four ([convert.go:222-241](../../pkg/ai/provider/openai/convert.go#L222)) |
| OpenAI Responses | ✅ | ✅ | ✅ | ✅ | ✅ all four ([convert.go:238-261](../../pkg/ai/provider/openairesponses/convert.go#L238)) |
| Google Gemini | ✅ AUTO | ✅ NONE | ✅ ANY | ⚠️ via `allowedFunctionNames` | ⚠️ |
| Gemini CLI | ✅ | ✅ | ✅ | ⚠️ | ⚠️ |

## Provider Documentation

- [Anthropic — Tool use](https://docs.anthropic.com/en/docs/build-with-claude/tool-use)
- [OpenAI — Function calling](https://platform.openai.com/docs/guides/function-calling)
- [OpenAI Responses — Tools](https://platform.openai.com/docs/guides/tools)
- [Google Gemini — Function calling](https://ai.google.dev/gemini-api/docs/function-calling)

## pi-go Gaps

- **Strict schema mode** plumbed only on OpenAI Chat via compat flag ([convert.go:205-206](../../pkg/ai/provider/openai/convert.go#L205)); not exposed as `ToolInfo` field, not passed to other providers.
- **`OutputSchema`** ([tool.go](../../pkg/ai/tool.go)) defined on `ToolInfo` but not forwarded by any provider.
- **`ToolInfo.Parallel`** flag exists but is not forwarded; `disable_parallel_tool_use` (Anthropic) / `parallel_tool_calls: false` (OpenAI) cannot be set.
- **Specific-tool forcing on Gemini** uses `allowedFunctionNames` (a list, in `ANY` mode); pi-go's single-string `ToolChoice` doesn't map cleanly.
- **Claude CLI** drops all tool definitions silently.
