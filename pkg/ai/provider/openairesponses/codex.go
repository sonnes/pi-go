package openairesponses

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openai/openai-go/option"

	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/openai"
)

// This file implements ChatGPT/Codex OAuth: the Responses provider routed
// through the Codex backend, plus detection from the OPENAI_OAUTH_TOKEN
// environment variable and from an existing Codex CLI login.

// openAICodexBaseURL is the ChatGPT/Codex Responses API mount. ChatGPT OAuth
// access tokens are honored only on this backend, not on the standard
// api.openai.com Chat Completions endpoint.
const openAICodexBaseURL = "https://chatgpt.com/backend-api/codex"

// chatgptAccountIDExtra is the [oauth.Credentials.Extras] key under which the
// Codex reuse tier stashes the ChatGPT account ID, so it survives token
// re-reads and can be applied as the chatgpt-account-id header.
const chatgptAccountIDExtra = "chatgpt_account_id"

// NewForCodexOAuth builds a Responses provider authenticated with a
// ChatGPT/Codex OAuth token. It routes through the Codex base URL because these
// tokens are rejected on the standard Chat Completions endpoint, and it sends
// the chatgpt-account-id header the Codex backend requires — read from
// accountID, or decoded from the token's JWT claims when accountID is empty.
// Optional refresh options persist rotated tokens.
func NewForCodexOAuth(
	clientID, accountID string,
	creds oauth.Credentials,
	refresh ...oauth.TransportOption,
) *Provider {
	transport := openai.NewOAuthTransport(clientID, creds, refresh...)
	opts := []option.RequestOption{
		option.WithBaseURL(openAICodexBaseURL),
		option.WithHTTPClient(&http.Client{Transport: transport}),
	}
	if accountID == "" {
		accountID = chatgptAccountID(creds.AccessToken)
	}
	if accountID != "" {
		opts = append(opts, option.WithHeader("chatgpt-account-id", accountID))
	}
	return NewForCodex(opts...)
}

// DetectOAuthEnv builds a Codex OAuth provider from OPENAI_OAUTH_TOKEN (with
// the optional OPENAI_OAUTH_CLIENT_ID) and reports whether it was set.
func DetectOAuthEnv() (*Provider, bool) {
	token := os.Getenv("OPENAI_OAUTH_TOKEN")
	if token == "" {
		return nil, false
	}
	clientID := os.Getenv("OPENAI_OAUTH_CLIENT_ID")
	return NewForCodexOAuth(clientID, "", oauth.Credentials{AccessToken: token}), true
}

// DetectCodexCLI builds a provider from an existing Codex CLI login and reports
// whether one was found. It reads the CLI's own $CODEX_HOME/auth.json store and
// re-reads it on refresh rather than running its own OAuth rotation.
func DetectCodexCLI() (*Provider, bool) {
	src := codexCLISource{}
	creds, err := src.load()
	if err != nil {
		return nil, false
	}
	accountID, _ := creds.Extras[chatgptAccountIDExtra].(string)
	return NewForCodexOAuth("", accountID, creds, oauth.WithRefresher(codexReReadRefresher(src))), true
}

// codexCLISource reads the Codex CLI's OAuth credentials from
// $CODEX_HOME/auth.json (default ~/.codex/auth.json).
type codexCLISource struct {
	// path overrides the auth.json location (for tests).
	path string
}

func (s codexCLISource) load() (oauth.Credentials, error) {
	path := s.path
	if path == "" {
		var err error
		if path, err = codexAuthPath(); err != nil {
			return oauth.Credentials{}, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return oauth.Credentials{}, err
	}
	return parseCodexCreds(data)
}

func codexAuthPath() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

// parseCodexCreds maps the Codex CLI's auth.json to [oauth.Credentials]. The
// schema is {"tokens":{"access_token","refresh_token","account_id","id_token"}}.
// Codex carries no explicit expiry, so it is derived from the access token's
// JWT "exp" claim. The account ID is stashed in Extras for the
// chatgpt-account-id header.
func parseCodexCreds(data []byte) (oauth.Credentials, error) {
	var doc struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
			IDToken      string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return oauth.Credentials{}, fmt.Errorf("openairesponses: parse codex auth: %w", err)
	}

	t := doc.Tokens
	if t.AccessToken == "" {
		return oauth.Credentials{}, fmt.Errorf("openairesponses: no codex access token found")
	}

	creds := oauth.Credentials{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    jwtExpiry(t.AccessToken),
	}
	if t.AccountID != "" {
		creds.Extras = map[string]any{chatgptAccountIDExtra: t.AccountID}
	}
	return creds, nil
}

// codexReReadRefresher returns a [oauth.TokenRefresher] that re-reads
// credentials from the Codex CLI's own store rather than running an HTTP
// refresh, surfacing a re-auth error if the re-read token is also expired.
func codexReReadRefresher(src codexCLISource) oauth.TokenRefresher {
	return oauth.TokenRefresherFunc(func(_ context.Context, _ oauth.Credentials) (oauth.Credentials, error) {
		creds, err := src.load()
		if err != nil {
			return oauth.Credentials{}, fmt.Errorf("openairesponses: reload Codex credentials: %w", err)
		}
		if creds.IsExpired() {
			return oauth.Credentials{}, fmt.Errorf(
				"openairesponses: Codex credentials are expired; re-authenticate with the Codex CLI",
			)
		}
		return creds, nil
	})
}

// chatgptAccountID extracts the ChatGPT account ID from an OpenAI OAuth access
// token. The token is a JWT whose payload carries the ID under the
// "https://api.openai.com/auth" claim. It returns "" if the token is not a
// well-formed JWT or the claim is absent.
func chatgptAccountID(token string) string {
	payload, err := jwtPayload(token)
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Auth.ChatGPTAccountID
}

// jwtExpiry returns the expiry encoded in a JWT's "exp" claim, or the zero time
// if the token is not a well-formed JWT or carries no exp.
func jwtExpiry(token string) time.Time {
	payload, err := jwtPayload(token)
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

// jwtPayload decodes the payload segment of a JWT. It returns an error if the
// token is not a three-segment JWT or the payload is not valid base64url.
func jwtPayload(token string) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("openairesponses: not a JWT")
	}
	return base64.RawURLEncoding.DecodeString(parts[1])
}
