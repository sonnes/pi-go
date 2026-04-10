package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openai/openai-go/option"
	"github.com/sonnes/pi-go/pkg/ai/oauth"
)

// TokenEndpoint is the default OpenAI OAuth token endpoint.
const TokenEndpoint = "https://auth.openai.com/oauth/token"

// Refresher implements [oauth.TokenRefresher] for OpenAI OAuth.
type Refresher struct {
	// Client is the HTTP client used for token requests.
	// If nil, [http.DefaultClient] is used.
	Client *http.Client
	// TokenURL overrides the default OpenAI token endpoint.
	// If empty, [TokenEndpoint] is used.
	TokenURL string
	// ClientID is the OAuth client ID. Required.
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

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {r.ClientID},
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return oauth.Credentials{}, fmt.Errorf("oauth: build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

	// Preserve the refresh token if the response doesn't include a new one.
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

// NewOAuthTransport creates an [oauth.Transport] configured for OpenAI
// OAuth with automatic token refresh.
func NewOAuthTransport(clientID string, creds oauth.Credentials, opts ...oauth.TransportOption) *oauth.Transport {
	defaults := []oauth.TransportOption{
		oauth.WithRefresher(&Refresher{ClientID: clientID}),
	}
	return oauth.NewTransport(creds, append(defaults, opts...)...)
}

// NewWithOAuth creates a new OpenAI provider configured for OAuth Bearer
// token authentication with automatic token refresh.
func NewWithOAuth(clientID string, creds oauth.Credentials, oauthOpts ...oauth.TransportOption) *Provider {
	transport := NewOAuthTransport(clientID, creds, oauthOpts...)
	return New(option.WithHTTPClient(&http.Client{Transport: transport}))
}
