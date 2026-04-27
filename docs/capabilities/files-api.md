---
title: "Files API"
summary: "Uploading files to a provider for reuse across requests"
read_when:
  - Reusing the same large document across many calls
  - Implementing File Search or RAG over uploaded content
---

# Files API

## Compatibility

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ Files API beta (`files-api-2025-04-14`) — 500 MB/file, 500 GB/org; PDF, DOCX, TXT, CSV, XLSX, MD, images | ❌ | no upload helper; `FileID`s accepted only as inputs ([convert.go:86-108](../../pkg/ai/provider/anthropic/convert.go#L86)) |
| OpenAI Chat | ✅ Files API + Vector Stores | ❌ | `FileID` accepted only ([convert.go:114](../../pkg/ai/provider/openai/convert.go#L114)) |
| OpenAI Responses | ✅ | ❌ | `FileID` accepted only |
| Google Gemini | ✅ Files API — 48h retention, free | ❌ | URI accepted as `FileData.fileUri` |
| Claude CLI | ❌ direct | — | |
| Gemini CLI | ⚠️ accepts file URIs | ❌ | |

## Provider Documentation

- [Anthropic — Files API](https://docs.anthropic.com/en/docs/build-with-claude/files)
- [OpenAI — Files](https://platform.openai.com/docs/api-reference/files)
- [Google Gemini — Files API](https://ai.google.dev/gemini-api/docs/files)

## pi-go Gaps

- No `FilesProvider` capability interface.
- No upload / list / delete helpers anywhere in the codebase.
- Without uploads, large reusable documents must be re-sent inline (base64) on every call — defeating the cost benefit.
- Vector Store management (OpenAI) and Cached Content (Google) are also missing.
