package oauth

import (
	"fmt"
	"net/http"
	"sync"
)

// Transport is an [http.RoundTripper] that injects OAuth Bearer tokens
// and transparently refreshes expired credentials before each request.
type Transport struct {
	// Base is the underlying RoundTripper. If nil, [http.DefaultTransport] is used.
	Base http.RoundTripper

	mu        sync.Mutex
	creds     Credentials
	refresher TokenRefresher
	onRefresh OnRefresh
	headers   map[string]string
}

// TransportOption configures a [Transport].
type TransportOption func(*Transport)

// WithBase sets the underlying [http.RoundTripper].
func WithBase(rt http.RoundTripper) TransportOption {
	return func(t *Transport) { t.Base = rt }
}

// WithRefresher sets the [TokenRefresher] used to refresh expired credentials.
func WithRefresher(r TokenRefresher) TransportOption {
	return func(t *Transport) { t.refresher = r }
}

// WithOnRefresh sets a callback invoked after a successful token refresh.
func WithOnRefresh(fn OnRefresh) TransportOption {
	return func(t *Transport) { t.onRefresh = fn }
}

// WithExtraHeaders sets additional headers injected into every request.
func WithExtraHeaders(h map[string]string) TransportOption {
	return func(t *Transport) { t.headers = h }
}

// NewTransport creates a Transport with the given credentials and options.
func NewTransport(creds Credentials, opts ...TransportOption) *Transport {
	t := &Transport{creds: creds}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// RoundTrip implements [http.RoundTripper]. It refreshes expired credentials
// (if a refresher is configured), then injects the Bearer token and extra
// headers into the request.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if t.creds.IsExpired() && t.refresher != nil {
		newCreds, err := t.refresher.RefreshToken(req.Context(), t.creds)
		if err != nil {
			t.mu.Unlock()
			return nil, fmt.Errorf("oauth: token refresh failed: %w", err)
		}
		t.creds = newCreds
		if t.onRefresh != nil {
			_ = t.onRefresh(newCreds)
		}
	}
	token := t.creds.AccessToken
	headers := t.headers
	t.mu.Unlock()

	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
