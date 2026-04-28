---
title: "Server-Side Tools"
summary: "Provider-hosted tools — web search, code execution, computer use, file search, MCP, tool search"
read_when:
  - Considering whether to wire any provider-hosted tool
  - Designing how pi-go represents non-function tools
---

# Server-Side Tools

Several providers ship tools the model can invoke that execute on the provider's infrastructure (web search, code execution, file search, computer use, etc.). The client never receives a `tool_use` block to act on; the result comes back as content. Tool discovery (Tool Search, MCP) is closely related and covered here too.

pi-go represents server tools as a variant of [`ai.ToolInfo`](../../pkg/ai/tool.go): set `Kind = ToolKindServer` and `ServerType` to one of the canonical [`ai.ServerToolType`](../../pkg/ai/tool.go) constants. The helper [`ai.DefineServerTool`](../../pkg/ai/tool.go) wraps a `ToolInfo` into an `ai.Tool` so it flows through the agent's standard `WithTools` plumbing alongside function tools. Each provider adapter maps these to its own typed configuration; unsupported types are silently skipped per provider.

```go
agent.New(
    agent.WithModel(model),
    agent.WithTools(
        ai.DefineServerTool(ai.ToolInfo{
            ServerType: ai.ServerToolWebSearch,
            ServerConfig: map[string]any{"max_uses": 5}, // Anthropic-specific
        }),
    ),
)
```

Provider-executed results arrive on the same `ai.ToolCall` that holds the invocation, with `Server == true` and `Output` populated by [`ai.ServerToolOutput`](../../pkg/ai/content.go). `Output.Content` is a normalized text rendering; `Output.Raw` retains the provider's JSON for callers that need structured fields. The `ToolCall.Name` is rewritten to the caller-registered `ToolInfo.Name` (e.g. `"WebSearch"`) rather than the raw provider item type (`web_search_call`, `openrouter:web_search`), so persisted history has the same shape for function tools and server tools.

## Overview

| Tool             | Anthropic  | OpenAI Chat | OpenAI Responses | Google         | OpenRouter         | Claude CLI | pi-go                                                  |
| ---------------- | ---------- | ----------- | ---------------- | -------------- | ------------------ | ---------- | ------------------------------------------------------ |
| Web search       | ✅         | ❌          | ✅               | ✅ (grounding) | ✅ via Responses    | ✅         | ✅ Anthropic / OpenAI Responses / Google / OpenRouter   |
| Web fetch        | ✅         | ❌          | ❌               | ❌             | ✅ via Responses    | ✅         | ✅ OpenRouter                                           |
| Code execution   | ✅         | ❌          | ✅               | ✅             | ❌                 | ✅         | ✅ OpenAI Responses / Google                           |
| Computer use     | ✅ preview | ❌          | ✅ preview       | ❌             | ❌                 | ⚠️         | ❌                                                     |
| Bash / shell     | ✅         | ❌          | ✅ hosted shell  | ❌             | ❌                 | ✅         | ❌                                                     |
| Text editor      | ✅         | ❌          | ❌               | ❌             | ❌                 | ✅         | ❌                                                     |
| File search      | ❌         | ❌          | ✅               | ⚠️ via Files   | ❌                 | ❌         | ❌                                                     |
| Apply patch      | ❌         | ❌          | ✅ V4A           | ❌             | ❌                 | ❌         | ❌                                                     |
| Image generation | ❌         | ❌          | ✅ inline        | ❌             | ✅ via Responses    | ❌         | ❌ deferred (overlaps `ai.ImageProvider`)              |
| Datetime         | ❌         | ❌          | ❌               | ❌             | ✅                 | ❌         | ✅ OpenRouter                                           |
| Tool search      | ✅         | ❌          | ✅               | ❌             | ❌                 | ✅         | ❌                                                     |
| MCP connector    | ✅         | ❌          | ✅               | ⚠️ via ACP     | ❌                 | ✅         | ❌                                                     |

## Web Search

Provider-hosted web search — model issues queries, provider executes, results come back as content the model can cite.

- **Anthropic** — `web_search_20260209`. [Docs](https://docs.anthropic.com/en/docs/build-with-claude/tool-use/web-search-tool)
- **OpenAI Responses** — `web_search` server tool. [Docs](https://platform.openai.com/docs/guides/tools-web-search)
- **Google Gemini** — `google_search` grounding. [Docs](https://ai.google.dev/gemini-api/docs/google-search)
- **Claude CLI** — `WebSearch` allowed-tool name; results not surfaced through pi-go.

## Web Fetch

Distinct from search: model supplies a URL, provider fetches it (PDF / HTML-extracted text), returns inline.

- **Anthropic** — `web_fetch_20260209`, dynamic content filtering on Mythos; no JS execution. [Docs](https://docs.anthropic.com/en/docs/build-with-claude/tool-use/web-fetch-tool)
- **Claude CLI** — `WebFetch` allowed-tool passthrough.

## Code Execution

Sandboxed Python/JavaScript REPL the model can use to compute, plot, or transform data.

- **Anthropic** — `code_execution_20250825` (all models), `code_execution_20260120` (Opus 4.5+ / Sonnet 4.5+) with REPL state and programmatic tool calling. [Docs](https://docs.anthropic.com/en/docs/build-with-claude/tool-use/code-execution-tool)
- **OpenAI Responses** — Code Interpreter. [Docs](https://platform.openai.com/docs/guides/tools-code-interpreter)
- **Google Gemini** — `code_execution` tool. [Docs](https://ai.google.dev/gemini-api/docs/code-execution)
- **Claude CLI** — uses `code_execution` internally; output not surfaced.

## Computer Use

Model directs a virtual desktop — screenshot, cursor, click, type, scroll. Host environment executes actions.

- **Anthropic** — `computer_20250124`, preview, Mac currently. [Docs](https://docs.anthropic.com/en/docs/build-with-claude/tool-use/computer-use-tool)
- **OpenAI Responses** — `computer_use_preview`. [Docs](https://platform.openai.com/docs/guides/tools-computer-use)

## Bash / Shell

Sandboxed bash environment.

- **Anthropic** — `bash_20250124` (recommended), `bash_20241022`. [Docs](https://docs.anthropic.com/en/docs/build-with-claude/tool-use/bash-tool)
- **OpenAI Responses** — hosted shell preview.
- **Claude CLI** — local sandboxed bash; pi-go only forwards `--allowedTools Bash` ([Sandboxing docs](https://docs.claude.com/en/docs/claude-code/sandboxing)).
- **Gemini CLI** — `run_shell_command`.

## Text Editor

Structured `view` / `create` / `str_replace` / `insert` / `undo_edit` API.

- **Anthropic** — `text_editor_20250728`. [Docs](https://docs.anthropic.com/en/docs/build-with-claude/tool-use/text-editor-tool)
- **Claude CLI** — native; pi-go forwards via `--allowedTools` only.

## File Search

Vector search over uploaded documents.

- **OpenAI Responses** — `file_search`. [Docs](https://platform.openai.com/docs/guides/tools-file-search). Depends on [files-api.md](files-api.md).
- **Google Gemini** — partial via Files API + grounding.

## Tool Search (dynamic discovery)

Rather than passing every tool definition every turn, the model queries a _catalog_. Anthropic claims ~85% token reduction.

- **Anthropic** — `tool_search_20241022`. [Docs](https://docs.anthropic.com/en/docs/agents-and-tools/tool-search-tool)
- **OpenAI Responses** — tool search.
- **Claude CLI** — tool search for MCP servers.

## MCP Connectors

Model Context Protocol — clients connect tool/resource/prompt servers and surface them to a model. Some providers accept MCP connector content blocks directly.

- **Anthropic** — MCP connector blocks. [Docs](https://docs.anthropic.com/en/docs/agents-and-tools/mcp)
- **OpenAI Responses** — MCP integration. [Docs](https://platform.openai.com/docs/guides/tools-remote-mcp)
- **Google / Gemini CLI** — via ACP bridging.
- [Model Context Protocol spec](https://modelcontextprotocol.io/specification)

## pi-go Wiring

The core abstraction lives in [`pkg/ai/tool.go`](../../pkg/ai/tool.go) and [`pkg/ai/content.go`](../../pkg/ai/content.go). Each provider adapter's `convertTools` branches on `ToolKind == ToolKindServer` and routes to a typed conversion helper:

- **Anthropic** ([`pkg/ai/provider/anthropic/convert.go`](../../pkg/ai/provider/anthropic/convert.go)) — `web_search` via the typed `WebSearchTool20250305Param`. Streaming pairs `server_tool_use` with `web_search_tool_result` and merges them into a single `ai.ToolCall` with `Output` populated. Other server tools (`code_execution`, `web_fetch`, computer/MCP) require the beta SDK and are skipped silently for now.
- **OpenAI Responses** ([`pkg/ai/provider/openairesponses/convert.go`](../../pkg/ai/provider/openairesponses/convert.go)) — `web_search` (`OfWebSearchPreview`) and `code_execution` (`OfCodeInterpreter`). Streaming uses `response.output_item.added` / `.done` for these item types and emits `EventToolStart` / `EventToolEnd` with `Server == true`.
- **Google Gemini** ([`pkg/ai/provider/google/google.go`](../../pkg/ai/provider/google/google.go)) — `web_search` toggles `Tool.GoogleSearch`; `code_execution` toggles `Tool.CodeExecution`. Function declarations and server tools are emitted as separate `Tool` entries because Gemini disallows mixing them. Decoding handles `ExecutableCode` / `CodeExecutionResult` parts inline and synthesizes a single trailing `ai.ToolCall` for `web_search` from the candidate's `GroundingMetadata`.
- **OpenRouter** — same package as OpenAI Responses, switched on via [`openairesponses.NewForOpenRouter`](../../pkg/ai/provider/openairesponses/openairesponses.go). The dialect translates server-tool kinds to the `openrouter:*` namespace (`openrouter:web_search`, `openrouter:web_fetch`, `openrouter:datetime`) and is injected into the request body via [`option.WithJSONSet`](https://pkg.go.dev/github.com/openai/openai-go/option#WithJSONSet). The SSE switch additionally handles `response.content_part.delta` (OpenRouter's text-delta event) and `response.output_item.added/.done` for items whose type starts with `openrouter:`. Server tools OpenRouter doesn't expose (code execution, file search, computer, MCP, bash, text editor) are dropped silently.

## Remaining Gaps

- **No host-side executor abstraction** for tools that need local execution (bash, text editor, computer use, apply_patch). The agent layer in [`pkg/agent`](../../pkg/agent) does not currently abstract these.
- **No dedicated content type for citations** beyond what's stuffed into `ServerToolOutput.Raw` — see [citations.md](citations.md).
- **No catalog primitive** for Tool Search.
- **No MCP client** in this repo; no bridging from a discovered MCP tool to `ai.ToolInfo`. The Responses adapter accepts `OfMcp` shape but pi-go does not yet expose it through `DefineServerTool`.
- **Anthropic non-`web_search` server tools** (`code_execution`, `web_fetch`, computer use) are not yet wired — they require migrating the Anthropic provider to the beta SDK path.
- Image generation as a server tool overlaps with the standalone [`ImageProvider`](../../pkg/ai/image.go) — see [multimodal-output.md](multimodal-output.md).
