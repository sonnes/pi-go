package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sonnes/pi-go/pkg/ai/oauth"
)

// TokenEndpoint is the default Anthropic OAuth token endpoint.
const TokenEndpoint = "https://platform.claude.com/v1/oauth/token"

// Refresher implements [oauth.TokenRefresher] for Anthropic OAuth.
type Refresher struct {
	// Client is the HTTP client for token requests. If Client is nil, the
	// Refresher uses [http.DefaultClient].
	Client *http.Client
	// TokenURL overrides the default Anthropic token endpoint. If TokenURL
	// is empty, the Refresher uses [TokenEndpoint].
	TokenURL string
	// ClientID is the OAuth client ID. It is required.
	ClientID string
}

// RefreshToken exchanges the refresh token in creds for a new access token.
func (r *Refresher) RefreshToken(ctx context.Context, creds oauth.Credentials) (oauth.Credentials, error) {
	tokenURL := r.TokenURL
	if tokenURL == "" {
		tokenURL = TokenEndpoint
	}
	if r.ClientID == "" {
		return oauth.Credentials{}, fmt.Errorf("oauth: ClientID is required")
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}

	payload := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": creds.RefreshToken,
		"client_id":     r.ClientID,
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return oauth.Credentials{}, fmt.Errorf("oauth: marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(string(jsonBody)),
	)
	if err != nil {
		return oauth.Credentials{}, fmt.Errorf("oauth: build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return oauth.Credentials{}, fmt.Errorf("oauth: refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return oauth.Credentials{}, fmt.Errorf("oauth: read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return oauth.Credentials{}, fmt.Errorf(
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
		return oauth.Credentials{}, fmt.Errorf("oauth: decode refresh response: %w", err)
	}

	// If the response does not include a new refresh token, keep the old one.
	refreshToken := tokenResp.RefreshToken
	if refreshToken == "" {
		refreshToken = creds.RefreshToken
	}

	return oauth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		Extras:       creds.Extras,
	}, nil
}

// OAuthHeaders returns the HTTP headers that Anthropic OAuth requests
// require.
func OAuthHeaders() map[string]string {
	return map[string]string{
		"anthropic-beta": "claude-code-20250219,oauth-2025-04-20",
		"x-app":          "cli",
	}
}

// NewOAuthTransport creates an [oauth.Transport] for Anthropic OAuth. The
// transport refreshes tokens automatically and sends the required headers.
func NewOAuthTransport(clientID string, creds oauth.Credentials, opts ...oauth.TransportOption) *oauth.Transport {
	defaults := []oauth.TransportOption{
		oauth.WithRefresher(&Refresher{ClientID: clientID}),
		oauth.WithExtraHeaders(OAuthHeaders()),
	}
	return oauth.NewTransport(creds, append(defaults, opts...)...)
}
