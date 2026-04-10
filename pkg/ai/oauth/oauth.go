// Package oauth provides optional OAuth authentication support for AI providers.
//
// The core abstraction is [Transport], an [http.RoundTripper] that injects
// Bearer tokens and transparently refreshes expired credentials before each
// request. Callers opt in by constructing a Transport and passing an
// [http.Client] that uses it to their provider.
//
// Provider-specific helpers like [NewAnthropicTransport] wire up the correct
// refresher and headers for a given provider.
package oauth

import (
	"context"
	"time"
)

// expiryMargin is the safety buffer before actual expiry during which
// credentials are considered expired and will be refreshed.
const expiryMargin = 5 * time.Minute

// Credentials holds OAuth token data.
// The Extras map allows provider-specific fields.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Extras       map[string]any
}

// IsExpired reports whether the access token has expired or will
// expire within the safety margin.
func (c Credentials) IsExpired() bool {
	return time.Now().After(c.ExpiresAt.Add(-expiryMargin))
}

// TokenRefresher exchanges a refresh token for new credentials.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, creds Credentials) (Credentials, error)
}

// TokenRefresherFunc adapts an ordinary function to the [TokenRefresher] interface.
type TokenRefresherFunc func(ctx context.Context, creds Credentials) (Credentials, error)

// RefreshToken calls f(ctx, creds).
func (f TokenRefresherFunc) RefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	return f(ctx, creds)
}

// OnRefresh is called after a successful token refresh, allowing the
// caller to persist updated credentials. Storage concerns stay outside
// the SDK.
type OnRefresh func(creds Credentials) error
