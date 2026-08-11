package oauth

import (
	"fmt"
	"net/http"
	"sync"
)

// Transport is an [http.RoundTripper] that adds an OAuth Bearer token to each
// request. It also refreshes expired credentials before the request runs.
type Transport struct {
	// Base is the underlying RoundTripper. If Base is nil, the Transport uses
	// [http.DefaultTransport].
	Base http.RoundTripper

	mu             sync.Mutex
	creds          Credentials
	refresher      TokenRefresher
	onRefresh      OnRefresh
	pendingPersist bool
	headers        map[string]string
}

// TransportOption configures a [Transport].
type TransportOption func(*Transport)

// WithBase sets the underlying [http.RoundTripper].
func WithBase(rt http.RoundTripper) TransportOption {
	return func(t *Transport) { t.Base = rt }
}

// WithRefresher sets the [TokenRefresher] that refreshes expired credentials.
func WithRefresher(r TokenRefresher) TransportOption {
	return func(t *Transport) { t.refresher = r }
}

// WithOnRefresh sets a callback that runs after a successful token refresh.
// If the callback returns an error, the request fails. The Transport runs the
// callback again before the next request.
func WithOnRefresh(fn OnRefresh) TransportOption {
	return func(t *Transport) { t.onRefresh = fn }
}

// WithExtraHeaders sets more headers that the Transport adds to every request.
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

// RoundTrip implements [http.RoundTripper]. If the credentials are expired and
// a refresher is set, RoundTrip refreshes them first. It then adds the Bearer
// token and the extra headers to the request.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if t.pendingPersist && t.onRefresh != nil {
		if err := t.onRefresh(t.creds); err != nil {
			t.mu.Unlock()
			return nil, fmt.Errorf("oauth: persist refreshed credentials: %w", err)
		}
		t.pendingPersist = false
	}
	if t.creds.IsExpired() && t.refresher != nil {
		newCreds, err := t.refresher.RefreshToken(req.Context(), t.creds)
		if err != nil {
			t.mu.Unlock()
			return nil, fmt.Errorf("oauth: token refresh failed: %w", err)
		}
		t.creds = newCreds
		if t.onRefresh != nil {
			if err := t.onRefresh(newCreds); err != nil {
				t.pendingPersist = true
				t.mu.Unlock()
				return nil, fmt.Errorf("oauth: persist refreshed credentials: %w", err)
			}
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
