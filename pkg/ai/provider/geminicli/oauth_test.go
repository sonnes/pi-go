package geminicli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefresher_RefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		assert.Contains(t, string(body), "grant_type=refresh_token")
		assert.Contains(t, string(body), "refresh_token=my-refresh")
		assert.Contains(t, string(body), "client_id=test-client-id")
		assert.Contains(t, string(body), "client_secret=test-client-secret")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	refresher := &Refresher{
		Client:       srv.Client(),
		TokenURL:     srv.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	creds := oauth.Credentials{
		RefreshToken: "my-refresh",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		Extras:       map[string]any{"projectId": "my-project"},
	}

	got, err := refresher.RefreshToken(t.Context(), creds)
	require.NoError(t, err)
	assert.Equal(t, "new-access", got.AccessToken)
	assert.Equal(t, "new-refresh", got.RefreshToken)
	assert.True(t, got.ExpiresAt.After(time.Now()))
	assert.Equal(t, "my-project", got.Extras["projectId"])
}

func TestRefresher_PreservesRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	refresher := &Refresher{
		Client:       srv.Client(),
		TokenURL:     srv.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	creds := oauth.Credentials{
		RefreshToken: "original-refresh",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
	}

	got, err := refresher.RefreshToken(t.Context(), creds)
	require.NoError(t, err)
	assert.Equal(t, "original-refresh", got.RefreshToken)
}

func TestRefresher_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer srv.Close()

	refresher := &Refresher{
		Client:       srv.Client(),
		TokenURL:     srv.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	_, err := refresher.RefreshToken(t.Context(), oauth.Credentials{RefreshToken: "bad"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestNewOAuthTransport(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := oauth.Credentials{
		AccessToken: "google-test-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	tr := NewOAuthTransport("test-client-id", "test-client-secret", creds)
	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer google-test-token", gotHeaders.Get("Authorization"))
}
