---
title: "Server-Side Tools"
summary: "Provider-hosted tools — web search, code execution, computer use, file search, MCP, tool search"
read_when:
  - Considering whether to wire any provider-hosted tool
  - Designing how pi-go represents non-function tools
---

# Server-Side Tools

Several providers ship tools the model can invoke that execute on the provider's infrastructure (web search, code execution, file search, computer use, etc.). The client never receives a `tool_use` block to act on; the result comes back as content. Tool discovery (Tool Search, MCP) is closely related and covered here too.

pi-go currently has **no representation for server tools**. [`ai.ToolInfo`](../../pkg/ai/tool.go) describes only client-side function tools. None of the entries below are wired in any pi-go provider.

## Overview

| Tool | Anthropic | OpenAI Chat | OpenAI Responses | Google | Claude CLI | pi-go |
|---|---|---|---|---|---|---|
| Web search | ✅ | ❌ | ✅ | ✅ (grounding) | ✅ | ❌ |
| Web fetch | ✅ | ❌ | ❌ | ❌ | ✅ | ❌ |
| Code execution | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ |
| Computer use | ✅ preview | ❌ | ✅ preview | ❌ | ⚠️ | ❌ |
| Bash / shell | ✅ | ❌ | ✅ hosted shell | ❌ | ✅ | ❌ |
| Text editor | ✅ | ❌ | ❌ | ❌ | ✅ | ❌ |
| File search | ❌ | ❌ | ✅ | ⚠️ via Files | ❌ | ❌ |
| Apply patch | ❌ | ❌ | ✅ V4A | ❌ | ❌ | ❌ |
| Image generation | ❌ | ❌ | ✅ inline | ❌ | ❌ | ❌ |
| Tool search | ✅ | ❌ | ✅ | ❌ | ✅ | ❌ |
| MCP connector | ✅ | ❌ | ✅ | ⚠️ via ACP | ✅ | ❌ |

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

Rather than passing every tool definition every turn, the model queries a *catalog*. Anthropic claims ~85% token reduction.

- **Anthropic** — `tool_search_20241022`. [Docs](https://docs.anthropic.com/en/docs/agents-and-tools/tool-search-tool)
- **OpenAI Responses** — tool search.
- **Claude CLI** — tool search for MCP servers.

## MCP Connectors

Model Context Protocol — clients connect tool/resource/prompt servers and surface them to a model. Some providers accept MCP connector content blocks directly.

- **Anthropic** — MCP connector blocks. [Docs](https://docs.anthropic.com/en/docs/agents-and-tools/mcp)
- **OpenAI Responses** — MCP integration. [Docs](https://platform.openai.com/docs/guides/tools-remote-mcp)
- **Google / Gemini CLI** — via ACP bridging.
- [Model Context Protocol spec](https://modelcontextprotocol.io/specification)

## pi-go Gaps

- **No abstraction for server tools at the core layer.** Adding any single one requires deciding whether to extend `ToolInfo` with a server-tool variant or introduce a separate `ServerTool` type.
- **No handling of provider-emitted server-tool result blocks** (e.g. Anthropic's `web_search_tool_result`, `code_execution_tool_result`).
- **No content type for citations or grounding metadata** that come back with search/fetch results — see [citations.md](citations.md).
- **No host-side executor abstraction** for tools that need local execution (bash, text editor, computer use, apply_patch). The agent layer in [`pkg/agent`](../../pkg/agent) does not currently abstract these.
- **No catalog primitive** for Tool Search.
- **No MCP client** in this repo; no bridging from a discovered MCP tool to `ai.ToolInfo`.
- Image generation as a server tool overlaps with the standalone [`ImageProvider`](../../pkg/ai/image.go) — see [multimodal-output.md](multimodal-output.md).
