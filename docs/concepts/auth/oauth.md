---
title: "OAuth"
summary: "OAuth transport middleware, token refresh, and Anthropic provider integration"
read_when:
  - Authenticating with OAuth tokens instead of API keys
  - Understanding how token refresh works
  - Connecting Anthropic provider with OAuth credentials
---

# OAuth

The `pkg/ai/oauth` package provides optional OAuth support as an HTTP transport layer. Providers continue to work with plain API keys — OAuth is strictly opt-in.

## Design: transport, not interface

OAuth is implemented as an `http.RoundTripper` middleware rather than a change to the `Provider` interface. This keeps OAuth concerns out of the core SDK and lets any provider that accepts a custom `http.Client` gain OAuth support for free.

The transport intercepts every outgoing HTTP request to:

1. Check whether the access token has expired (with a 5-minute safety margin)
2. Refresh the token if needed, using a provider-specific `TokenRefresher`
3. Inject the `Authorization: Bearer <token>` header
4. Inject any provider-specific headers (e.g. `anthropic-beta: oauth-2025-04-20`)

Refresh is mutex-protected — concurrent requests wait for a single refresh rather than racing.

## Design: credentials are opaque to providers

Providers receive an `oauth.Credentials` value and don't know or care how it was obtained. Credentials might come from a login flow, a file on disk, an environment variable, or a test fixture. This separation keeps the provider layer focused on API calls and pushes authentication orchestration to the application layer.

## Token refresh and persistence

The `OnRefresh` callback is called after every successful token refresh, allowing the application to persist updated credentials. The SDK deliberately does not define _where_ credentials are stored — that's an application concern.

The `TokenRefresher` interface has a single method: exchange a set of credentials for a new set. Each provider (Anthropic, etc.) implements its own refresher that knows the correct token endpoint and request format.

## Anthropic integration

The Anthropic provider exposes a single `WithOAuth(creds, ...opts)` option that wires up everything:

- Sets `option.WithAuthToken` on the SDK client (Bearer auth instead of `x-api-key`)
- Creates an `oauth.Transport` with the Anthropic refresher and OAuth-specific headers
- Wraps any existing `http.Client` transport (if one was provided via `WithHTTPClient`)

This means the caller doesn't need to manually construct a transport, inject headers, or coordinate between the SDK client and the HTTP client.

## Application-level concerns

The following are intentionally outside the SDK:

- **Token detection** — checking whether a string is an OAuth token (e.g. `sk-ant-oat` prefix) belongs in the application layer, not the SDK.
- **Environment variable resolution** — which env vars to check and in what order is app-specific.
- **Login flows** — OAuth authorization (PKCE, callback servers, browser redirects) is interactive and belongs in a CLI or UI layer.
- **Credential storage** — file-based, keychain, or otherwise is the application's choice.

## Related

- [Providers](/concepts/ai/providers) — the provider interface that OAuth layers beneath
- [Options](/concepts/ai/options) — `WithHeaders` for per-request header injection
