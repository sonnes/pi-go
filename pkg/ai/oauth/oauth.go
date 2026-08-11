// Package oauth adds optional OAuth authentication for AI providers.
//
// The core type is [Transport], an [http.RoundTripper]. It adds a Bearer token
// to each request. It also refreshes expired credentials before the request
// runs. The caller builds a Transport and gives the provider an [http.Client]
// that uses it.
//
// Provider-specific helpers, for example NewAnthropicTransport, live in their
// provider packages. Each helper sets the correct refresher and headers for
// that provider.
package oauth

import (
	"context"
	"time"
)

// expiryMargin is the time before the true expiry when credentials count as
// expired. The transport refreshes credentials inside this window.
const expiryMargin = 5 * time.Minute

// Credentials holds OAuth token data.
// The Extras map holds provider-specific fields.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Extras       map[string]any
}

// IsExpired reports whether the access token is expired, or expires inside
// the safety margin.
func (c Credentials) IsExpired() bool {
	return time.Now().After(c.ExpiresAt.Add(-expiryMargin))
}

// TokenRefresher exchanges a refresh token for new credentials.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, creds Credentials) (Credentials, error)
}

// TokenRefresherFunc adapts an ordinary function to the [TokenRefresher]
// interface.
type TokenRefresherFunc func(ctx context.Context, creds Credentials) (Credentials, error)

// RefreshToken calls f(ctx, creds).
func (f TokenRefresherFunc) RefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	return f(ctx, creds)
}

// OnRefresh runs after a successful token refresh. The caller uses it to store
// the new credentials. If OnRefresh returns an error, the request does not use
// the rotated credentials. The SDK does not store credentials itself.
type OnRefresh func(creds Credentials) error
