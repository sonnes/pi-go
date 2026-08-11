package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransport_InjectsBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := NewTransport(
		Credentials{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		},
	)

	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer test-token", gotAuth)
}

func TestTransport_InjectsExtraHeaders(t *testing.T) {
	var gotBeta, gotApp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		gotApp = r.Header.Get("x-app")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := NewTransport(
		Credentials{
			AccessToken: "tok",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		},
		WithExtraHeaders(map[string]string{
			"anthropic-beta": "oauth-2025-04-20",
			"x-app":          "cli",
		}),
	)

	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "oauth-2025-04-20", gotBeta)
	assert.Equal(t, "cli", gotApp)
}

func TestTransport_RefreshesExpiredToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	refreshed := Credentials{
		AccessToken: "refreshed-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	tr := NewTransport(
		Credentials{
			AccessToken:  "expired-token",
			RefreshToken: "refresh-me",
			ExpiresAt:    time.Now().Add(-1 * time.Hour), // already expired
		},
		WithRefresher(TokenRefresherFunc(
			func(ctx context.Context, creds Credentials) (Credentials, error) {
				return refreshed, nil
			},
		)),
	)

	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer refreshed-token", gotAuth)
}

func TestTransport_CallsOnRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var persisted Credentials
	tr := NewTransport(
		Credentials{
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(-1 * time.Hour),
		},
		WithRefresher(TokenRefresherFunc(
			func(ctx context.Context, creds Credentials) (Credentials, error) {
				return Credentials{
					AccessToken: "new",
					ExpiresAt:   time.Now().Add(1 * time.Hour),
				}, nil
			},
		)),
		WithOnRefresh(func(creds Credentials) error {
			persisted = creds
			return nil
		}),
	)

	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "new", persisted.AccessToken)
}

func TestTransport_SurfacesOnRefreshFailure(t *testing.T) {
	persistErr := assert.AnError
	tr := NewTransport(
		Credentials{
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(-1 * time.Hour),
		},
		WithRefresher(TokenRefresherFunc(
			func(context.Context, Credentials) (Credentials, error) {
				return Credentials{
					AccessToken: "new",
					ExpiresAt:   time.Now().Add(1 * time.Hour),
				}, nil
			},
		)),
		WithOnRefresh(func(Credentials) error {
			return persistErr
		}),
	)

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"https://example.com",
		nil,
	)
	require.NoError(t, err)

	_, err = tr.RoundTrip(req)

	require.Error(t, err)
	assert.ErrorIs(t, err, persistErr)
	assert.Contains(t, err.Error(), "persist refreshed credentials")
}

func TestTransport_RetriesPersistenceWithoutRefreshingAgain(t *testing.T) {
	var refreshCount, persistCount int
	var gotAuth string
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	tr := NewTransport(
		Credentials{
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(-1 * time.Hour),
		},
		WithBase(base),
		WithRefresher(TokenRefresherFunc(
			func(context.Context, Credentials) (Credentials, error) {
				refreshCount++
				return Credentials{
					AccessToken: "new",
					ExpiresAt:   time.Now().Add(1 * time.Hour),
				}, nil
			},
		)),
		WithOnRefresh(func(Credentials) error {
			persistCount++
			if persistCount == 1 {
				return assert.AnError
			}
			return nil
		}),
	)

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"https://example.com",
		nil,
	)
	require.NoError(t, err)

	_, err = tr.RoundTrip(req)
	require.Error(t, err)

	res, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, 1, refreshCount)
	assert.Equal(t, 2, persistCount)
	assert.Equal(t, "Bearer new", gotAuth)
}

func TestTransport_NoRefreshWhenValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	refreshCalled := false
	tr := NewTransport(
		Credentials{
			AccessToken: "valid-token",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		},
		WithRefresher(TokenRefresherFunc(
			func(ctx context.Context, creds Credentials) (Credentials, error) {
				refreshCalled = true
				return creds, nil
			},
		)),
	)

	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.False(t, refreshCalled)
}

func TestTransport_ConcurrentAccess(t *testing.T) {
	var reqCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var refreshCount atomic.Int64
	tr := NewTransport(
		Credentials{
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(-1 * time.Hour),
		},
		WithRefresher(TokenRefresherFunc(
			func(ctx context.Context, creds Credentials) (Credentials, error) {
				refreshCount.Add(1)
				return Credentials{
					AccessToken: "new",
					ExpiresAt:   time.Now().Add(1 * time.Hour),
				}, nil
			},
		)),
	)

	client := &http.Client{Transport: tr}
	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			resp, err := client.Get(srv.URL)
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(n), reqCount.Load())
	// Refresh should only happen once due to mutex
	assert.Equal(t, int64(1), refreshCount.Load())
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
