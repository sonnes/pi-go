package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogin_Success(t *testing.T) {
	// Mock token endpoint.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		body := r.FormValue("grant_type")
		assert.Equal(t, "authorization_code", body)
		assert.NotEmpty(t, r.FormValue("code"))
		assert.NotEmpty(t, r.FormValue("code_verifier"))
		assert.Equal(t, "test-client-id", r.FormValue("client_id"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	// Capture the authorize URL to extract state for the callback.
	var authorizeURL string
	cfg := LoginConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenSrv.URL,
		ClientID:     "test-client-id",
		RedirectPort: 18931, // High port unlikely to conflict.
		Scopes:       []string{"openid", "profile"},
		DisplayURL: func(u string) error {
			authorizeURL = u
			// Simulate the browser redirect by hitting the callback server.
			go func() {
				// Parse the state from the authorize URL.
				parsed, _ := url.Parse(u)
				state := parsed.Query().Get("state")

				cbURL := fmt.Sprintf(
					"http://localhost:18931/callback?code=test-code&state=%s",
					url.QueryEscape(state),
				)
				resp, err := http.Get(cbURL)
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	creds, err := Login(ctx, cfg)
	require.NoError(t, err)
	assert.Equal(t, "new-access", creds.AccessToken)
	assert.Equal(t, "new-refresh", creds.RefreshToken)
	assert.True(t, creds.ExpiresAt.After(time.Now()))
	assert.NotEmpty(t, authorizeURL)
}

func TestLogin_StateMismatch(t *testing.T) {
	cfg := LoginConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     "https://example.com/token",
		ClientID:     "test-client-id",
		RedirectPort: 18932,
		Scopes:       []string{"openid"},
		DisplayURL: func(u string) error {
			go func() {
				cbURL := "http://localhost:18932/callback?code=test-code&state=wrong-state"
				resp, err := http.Get(cbURL)
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	_, err := Login(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestLogin_Timeout(t *testing.T) {
	cfg := LoginConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     "https://example.com/token",
		ClientID:     "test-client-id",
		RedirectPort: 18933,
		Scopes:       []string{"openid"},
		DisplayURL:   func(u string) error { return nil },
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	_, err := Login(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestLogin_TokenEndpointError(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer tokenSrv.Close()

	cfg := LoginConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenSrv.URL,
		ClientID:     "test-client-id",
		RedirectPort: 18934,
		Scopes:       []string{"openid"},
		DisplayURL: func(u string) error {
			go func() {
				parsed, _ := url.Parse(u)
				state := parsed.Query().Get("state")
				cbURL := fmt.Sprintf(
					"http://localhost:18934/callback?code=test-code&state=%s",
					url.QueryEscape(state),
				)
				resp, err := http.Get(cbURL)
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, err := Login(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestParsePastedCode(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCode  string
		wantState string
	}{
		{
			name:     "bare code",
			input:    "auth-code-123",
			wantCode: "auth-code-123",
		},
		{
			name:     "trims whitespace",
			input:    "  auth-code-123\n",
			wantCode: "auth-code-123",
		},
		{
			name:      "full redirect URL",
			input:     "http://localhost:1455/auth/callback?code=abc&state=xyz",
			wantCode:  "abc",
			wantState: "xyz",
		},
		{
			name:     "full redirect URL without state",
			input:    "http://localhost:1455/auth/callback?code=abc",
			wantCode: "abc",
		},
		{
			name:      "code#state form",
			input:     "abc#xyz",
			wantCode:  "abc",
			wantState: "xyz",
		},
		{
			name:  "empty",
			input: "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, state := parsePastedCode(tt.input)
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, tt.wantState, state)
		})
	}
}

// TestLogin_ManualPaste verifies the flow completes via the ReadCode paste
// callback when no localhost callback arrives.
func TestLogin_ManualPaste(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "pasted-code", r.FormValue("code"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "paste-access",
			"refresh_token": "paste-refresh",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	var captured string
	cfg := LoginConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenSrv.URL,
		ClientID:     "test-client-id",
		RedirectPort: 18936,
		Scopes:       []string{"openid"},
		DisplayURL:   func(u string) error { return nil },
		ReadCode: func(ctx context.Context) (string, error) {
			// Paste a full redirect URL carrying the matching state.
			parsed, _ := url.Parse(captured)
			state := parsed.Query().Get("state")
			return fmt.Sprintf(
				"http://localhost:18936/callback?code=pasted-code&state=%s",
				url.QueryEscape(state),
			), nil
		},
	}
	cfg.DisplayURL = func(u string) error {
		captured = u
		return nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	creds, err := Login(ctx, cfg)
	require.NoError(t, err)
	assert.Equal(t, "paste-access", creds.AccessToken)
}

// TestLogin_ManualPaste_ListenFailure verifies that when the localhost
// callback port is unavailable, login still completes via paste.
func TestLogin_ManualPaste_ListenFailure(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "paste-access",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	// Occupy the callback port so net.Listen fails.
	blocker, err := net.Listen("tcp", "127.0.0.1:18937")
	require.NoError(t, err)
	defer blocker.Close()

	cfg := LoginConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenSrv.URL,
		ClientID:     "test-client-id",
		RedirectPort: 18937,
		Scopes:       []string{"openid"},
		DisplayURL:   func(u string) error { return nil },
		ReadCode: func(ctx context.Context) (string, error) {
			return "bare-pasted-code", nil
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	creds, err := Login(ctx, cfg)
	require.NoError(t, err)
	assert.Equal(t, "paste-access", creds.AccessToken)
}

func TestLogin_WithClientSecret(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-secret", r.FormValue("client_secret"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	cfg := LoginConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenSrv.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectPort: 18935,
		Scopes:       []string{"openid"},
		DisplayURL: func(u string) error {
			go func() {
				parsed, _ := url.Parse(u)
				state := parsed.Query().Get("state")
				cbURL := fmt.Sprintf(
					"http://localhost:18935/callback?code=test-code&state=%s",
					url.QueryEscape(state),
				)
				resp, err := http.Get(cbURL)
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	creds, err := Login(ctx, cfg)
	require.NoError(t, err)
	assert.Equal(t, "new-access", creds.AccessToken)
}
