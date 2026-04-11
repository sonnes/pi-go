---
title: "OAuth"
summary: "OAuth transport middleware, login flow, token refresh, and provider integration for Anthropic, OpenAI, and Gemini CLI"
read_when:
  - Authenticating with OAuth tokens instead of API keys
  - Understanding how token refresh works
  - Adding OAuth login to a CLI application
  - Connecting any provider with OAuth credentials
---

# OAuth

The `pkg/ai/oauth` package provides optional OAuth support as an HTTP transport layer and a reusable login flow. Providers continue to work with plain API keys — OAuth is strictly opt-in.

## Design: transport, not interface

OAuth is implemented as an `http.RoundTripper` middleware rather than a change to the `Provider` interface. This keeps OAuth concerns out of the core SDK and lets any provider that accepts a custom `http.Client` gain OAuth support for free.

The transport intercepts every outgoing HTTP request to:

1. Check whether the access token has expired (with a 5-minute safety margin)
2. Refresh the token if needed, using a provider-specific `TokenRefresher`
3. Inject the `Authorization: Bearer <token>` header
4. Inject any provider-specific headers (e.g. Anthropic's `anthropic-beta` header)

Refresh is mutex-protected — concurrent requests wait for a single refresh rather than racing.

## Design: credentials are opaque to providers

Providers receive an `oauth.Credentials` value and don't know or care how it was obtained. Credentials might come from a login flow, a file on disk, an environment variable, or a test fixture. This separation keeps the provider layer focused on API calls and pushes authentication orchestration to the application layer.

## Design: no hardcoded client IDs

OAuth client IDs and secrets are never embedded in the SDK. Each provider's `Refresher` requires them as explicit fields, and convenience constructors like `WithOAuth` accept them as parameters. The application layer is responsible for sourcing these values (from environment variables, config files, etc.).

## Login flow

The `oauth` package provides a reusable `Login(ctx, LoginConfig)` function that implements the full OAuth authorization code flow with PKCE:

1. Generates a PKCE verifier and S256 challenge (`GeneratePKCE()`)
2. Starts a local HTTP callback server on a configured port
3. Builds the authorize URL with all required parameters
4. Calls a `DisplayURL` callback (injected by the application) to present the URL
5. Waits for the browser callback with the authorization code
6. Exchanges the code for tokens at the provider's token endpoint
7. Returns `Credentials` with access token, refresh token, and expiry

Each provider exposes a `LoginConfig(clientID, ...)` function that returns a pre-filled `LoginConfig` with the correct endpoints, ports, scopes, and token exchange format. The application only needs to set the `DisplayURL` callback and call `oauth.Login`.

Provider-specific login details (e.g. Anthropic uses JSON token exchange with state, OpenAI uses form-encoded without state) are captured in `LoginConfig` fields like `UseJSONTokenRequest` and `IncludeStateInTokenExchange`.

## Token refresh and persistence

The `OnRefresh` callback is called after every successful token refresh, allowing the application to persist updated credentials. The SDK deliberately does not define _where_ credentials are stored — that's an application concern.

The `TokenRefresher` interface has a single method: exchange a set of credentials for a new set. Each provider implements its own refresher in its own package, with the correct token endpoint and request format. All refreshers preserve the original refresh token if the server response omits a new one.

## Design: refreshers live with their providers

Provider-specific OAuth code (refreshers, transport constructors, login configs, extra headers) lives in the respective provider package, not in `pkg/ai/oauth`. The `oauth` package is a generic toolkit — `Credentials`, `Transport`, `TokenRefresher`, `PKCE`, `Login`, and functional options. This keeps the dependency graph clean: each provider depends on `oauth`, but `oauth` has no knowledge of any provider.

## Provider integrations

### Anthropic

`WithOAuth(clientID, creds, ...opts)` wires up everything:

- Sets `option.WithAuthToken` on the SDK client (Bearer auth instead of `x-api-key`)
- Creates an `oauth.Transport` with the Anthropic refresher and OAuth-specific headers (`anthropic-beta`, `x-app`)
- Wraps any existing `http.Client` transport (if one was provided via `WithHTTPClient`)
- Token exchange uses JSON, includes state parameter

Token endpoint: `https://platform.claude.com/v1/oauth/token`

### OpenAI

`NewWithOAuth(clientID, creds, ...opts)` creates a provider with OAuth:

- Creates an `oauth.Transport` with the OpenAI refresher
- Passes the resulting `http.Client` to the OpenAI SDK
- Token exchange uses form-encoded, no state parameter

Token endpoint: `https://auth.openai.com/oauth/token`

### Gemini CLI

`WithOAuth(clientID, clientSecret, creds, ...opts)` configures OAuth on the Gemini CLI provider (`pkg/ai/provider/geminicli`):

- Creates an `oauth.Transport` with the Google refresher
- Requires `client_secret` in addition to `client_id` (standard for Google's installed-app OAuth flow)
- Token exchange uses form-encoded

Token endpoint: `https://oauth2.googleapis.com/token`

Note: The `google` provider (`pkg/ai/provider/google`) uses API keys only and does not support OAuth. OAuth for Google services routes through the Gemini CLI provider which uses the Cloud Code Assist API.

## CLI integration

The `cmd/pi` CLI demonstrates the full OAuth lifecycle:

- `pi login <provider>` — runs the login flow, stores credentials at `~/.pigo/auth.json`
- `pi logout <provider>` — removes stored credentials
- Provider detection checks `~/.pigo/auth.json` before falling back to environment variables
- Refreshed tokens are automatically persisted back to `auth.json` via `OnRefresh`
- The `--provider` flag selects which stored provider to use when multiple are available

## Application-level concerns

The following are intentionally outside the SDK:

- **Client IDs and secrets** — these are application configuration, not SDK constants.
- **Token detection** — checking whether a string is an OAuth token (e.g. `sk-ant-oat` prefix) belongs in the application layer, not the SDK.
- **Environment variable resolution** — which env vars to check and in what order is app-specific.
- **Credential storage** — file-based, keychain, or otherwise is the application's choice.

## Related

- [Providers](/concepts/ai/providers) — the provider interface that OAuth layers beneath
- [Options](/concepts/ai/options) — `WithHeaders` for per-request header injection
