---
title: "Authentication"
summary: "API keys, OAuth flows, service accounts, cloud-native endpoints"
read_when:
  - Bootstrapping a provider with credentials
  - Adding OAuth or service-account auth to a new provider
---

# Authentication

See also [docs/concepts/auth/oauth.md](../concepts/auth/oauth.md) for the design of pi-go's OAuth transport. CLI credentials are stored at `~/.pigo/auth.json` per [CLAUDE.md](../../CLAUDE.md).

## API Key

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ | ✅ | `WithAPIKey` ([anthropic.go:372-374](../../pkg/ai/provider/anthropic/anthropic.go#L372)) |
| OpenAI Chat | ✅ | ✅ | passed via `option.RequestOption` |
| OpenAI Responses | ✅ | ✅ | |
| Google Gemini | ✅ | ✅ | `WithAPIKey` ([google.go:39-41](../../pkg/ai/provider/google/google.go#L39)) |
| Claude CLI | ✅ via `ANTHROPIC_API_KEY` env | ✅ | inherits process env |
| Gemini CLI | ❌ (OAuth only) | ❌ | |

## OAuth

| Provider | API | pi-go | Notes |
|---|---|---|---|
| Anthropic | ✅ Claude.ai OAuth | ✅ | endpoint `https://platform.claude.com/v1/oauth/token`, beta header `claude-code-20250219,oauth-2025-04-20` ([oauth.go:111](../../pkg/ai/provider/anthropic/oauth.go#L111)) |
| OpenAI Chat | ✅ ChatGPT OAuth | ✅ | `NewWithOAuth` ([oauth.go:114-119](../../pkg/ai/provider/openai/oauth.go#L114)); endpoint `https://auth.openai.com/oauth/token` |
| OpenAI Responses | ⚠️ via the same OAuth as Chat | ❌ | not implemented in this provider |
| Google Gemini | ✅ Vertex OAuth (gcloud) | ❌ | `pkg/ai/provider/google` is API-key only |
| Claude CLI | ✅ delegated to CLI's own login | ✅ | |
| Gemini CLI | ✅ Bearer token from Cloud Code Assist OAuth | ✅ | `Credentials{Token, ProjectID}` ([geminicli.go:29-32](../../pkg/ai/provider/geminicli/geminicli.go#L29)) |

## Service Account / Cloud Endpoints

| Variant | API | pi-go | Notes |
|---|---|---|---|
| Anthropic on Bedrock | ✅ | ❌ | not implemented |
| Anthropic on Vertex | ✅ | ❌ | not implemented |
| OpenAI on Azure | ✅ Azure OpenAI | ❌ | not implemented |
| Google on Vertex AI | ✅ ADC / service account | ❌ | `pkg/ai/provider/google` uses `generativelanguage.googleapis.com` only |
| Gemini CLI | ✅ pre-issued bearer | ✅ | ([geminicli.go:179](../../pkg/ai/provider/geminicli/geminicli.go#L179)) |

## Provider Documentation

- [Anthropic — API keys](https://docs.anthropic.com/en/api/getting-started)
- [Anthropic — OAuth applications](https://docs.anthropic.com/en/api/oauth-applications)
- [Anthropic on Bedrock](https://docs.anthropic.com/en/api/claude-on-amazon-bedrock)
- [Anthropic on Vertex](https://docs.anthropic.com/en/api/claude-on-vertex-ai)
- [OpenAI — Authentication](https://platform.openai.com/docs/api-reference/authentication)
- [Azure OpenAI](https://learn.microsoft.com/en-us/azure/ai-services/openai/)
- [Google Gemini — API keys](https://ai.google.dev/gemini-api/docs/api-key)
- [Google — gcloud OAuth](https://cloud.google.com/docs/authentication/gcloud)
- [Google Vertex AI auth](https://cloud.google.com/vertex-ai/docs/authentication)

## pi-go Gaps

- **OpenAI Responses provider** has no `NewWithOAuth` helper; would need to mirror the wiring from `openai`.
- **Google HTTP provider** has no OAuth or service-account helper; only `geminicli` does.
- **No Bedrock / Vertex routing** for Anthropic models.
- **No Azure OpenAI** variant.
- **Login UX** (browser redirect, code capture) lives in `cmd/pi`; not exposed as a reusable library entry point.
