package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// AnthropicTokenURL is the default Anthropic OAuth token endpoint.
	AnthropicTokenURL = "https://auth.anthropic.com/oauth/token"
	// AnthropicClientID is the public OAuth client ID for CLI-based flows.
	AnthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

// AnthropicRefresher implements [TokenRefresher] for Anthropic OAuth.
type AnthropicRefresher struct {
	// Client is the HTTP client used for token requests.
	// If nil, [http.DefaultClient] is used.
	Client *http.Client
	// TokenURL overrides the default Anthropic token endpoint.
	// If empty, [AnthropicTokenURL] is used.
	TokenURL string
	// ClientID overrides the default OAuth client ID.
	// If empty, [AnthropicClientID] is used.
	ClientID string
}

// RefreshToken exchanges the refresh token in creds for a new access token.
func (r *AnthropicRefresher) RefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	tokenURL := r.TokenURL
	if tokenURL == "" {
		tokenURL = AnthropicTokenURL
	}
	clientID := r.ClientID
	if clientID == "" {
		clientID = AnthropicClientID
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {clientID},
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return Credentials{}, fmt.Errorf("oauth: build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("oauth: refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Credentials{}, fmt.Errorf("oauth: read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf(
			"oauth: refresh failed with status %d: %s",
			resp.StatusCode,
			string(body),
		)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return Credentials{}, fmt.Errorf("oauth: decode refresh response: %w", err)
	}

	return Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		Extras:       creds.Extras,
	}, nil
}

// AnthropicOAuthHeaders returns the HTTP headers required for
// Anthropic OAuth requests.
func AnthropicOAuthHeaders() map[string]string {
	return map[string]string{
		"anthropic-beta": "claude-code-20250219,oauth-2025-04-20",
		"x-app":          "cli",
	}
}

// NewAnthropicTransport creates a [Transport] configured for Anthropic
// OAuth with automatic token refresh and the required headers.
func NewAnthropicTransport(creds Credentials, opts ...TransportOption) *Transport {
	defaults := []TransportOption{
		WithRefresher(&AnthropicRefresher{}),
		WithExtraHeaders(AnthropicOAuthHeaders()),
	}
	return NewTransport(creds, append(defaults, opts...)...)
}
