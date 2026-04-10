package oauth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicRefresher_RefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		assert.Contains(t, string(body), "grant_type=refresh_token")
		assert.Contains(t, string(body), "refresh_token=my-refresh")
		assert.Contains(t, string(body), "client_id="+AnthropicClientID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	refresher := &AnthropicRefresher{
		Client:   srv.Client(),
		TokenURL: srv.URL,
	}

	creds := Credentials{
		RefreshToken: "my-refresh",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
	}

	got, err := refresher.RefreshToken(t.Context(), creds)
	require.NoError(t, err)
	assert.Equal(t, "new-access", got.AccessToken)
	assert.Equal(t, "new-refresh", got.RefreshToken)
	assert.True(t, got.ExpiresAt.After(time.Now()))
}

func TestAnthropicRefresher_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer srv.Close()

	refresher := &AnthropicRefresher{
		Client:   srv.Client(),
		TokenURL: srv.URL,
	}

	_, err := refresher.RefreshToken(t.Context(), Credentials{RefreshToken: "bad"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAnthropicOAuthHeaders(t *testing.T) {
	h := AnthropicOAuthHeaders()
	assert.Contains(t, h["anthropic-beta"], "oauth-2025-04-20")
	assert.Equal(t, "cli", h["x-app"])
}

func TestNewAnthropicTransport(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := Credentials{
		AccessToken: "sk-ant-oat01-test",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	tr := NewAnthropicTransport(creds)
	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer sk-ant-oat01-test", gotHeaders.Get("Authorization"))
	assert.Contains(t, gotHeaders.Get("anthropic-beta"), "oauth-2025-04-20")
	assert.Equal(t, "cli", gotHeaders.Get("x-app"))
}
