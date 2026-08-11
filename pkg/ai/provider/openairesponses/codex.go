package openairesponses

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openai/openai-go/v3/option"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/openai"
)

// This file implements ChatGPT and Codex OAuth. It routes the Responses
// provider through the Codex backend. It also detects credentials from the
// OPENAI_OAUTH_TOKEN environment variable and from an existing Codex CLI
// login.

// openAICodexBaseURL is the mount point of the ChatGPT Codex Responses API.
// This backend is the only one that accepts ChatGPT OAuth access tokens. The
// standard api.openai.com Chat Completions endpoint rejects them.
const (
	openAICodexBaseURL = "https://chatgpt.com/backend-api/codex"
	// codexModelsClientVersion is the compatibility version that Codex
	// source builds use. The models endpoint requires a semantic client
	// version. The value 0.0.0 selects the current development catalog.
	// This package reads only the stable fields of that catalog.
	codexModelsClientVersion = "0.0.0"
)

// chatgptAccountIDExtra is the [oauth.Credentials.Extras] key that holds the
// ChatGPT account ID for the Codex reuse tier. The ID survives a token
// re-read. The provider sends it as the chatgpt-account-id header.
const chatgptAccountIDExtra = "chatgpt_account_id"

// NewForCodexOAuth builds a Responses provider that authenticates with a
// ChatGPT Codex OAuth token. It routes through the Codex base URL because
// the standard Chat Completions endpoint rejects these tokens. It also sends
// the chatgpt-account-id header that the Codex backend requires. The value
// comes from accountID. If accountID is empty, the value comes from the JWT
// claims of the token. The optional refresh options store rotated tokens.
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

// ListCodexModels returns the models that the authenticated ChatGPT account
// can use through the Codex backend. It omits a model that the picker hides
// or that the API does not support. The optional transport options can store
// refreshed credentials. They can also replace the HTTP transport in tests.
func ListCodexModels(
	ctx context.Context,
	clientID, accountID string,
	creds oauth.Credentials,
	refresh ...oauth.TransportOption,
) ([]ai.Model, error) {
	transport := openai.NewOAuthTransport(clientID, creds, refresh...)
	client := &http.Client{Transport: transport}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		openAICodexBaseURL+"/models",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("openairesponses: build Codex models request: %w", err)
	}
	query := req.URL.Query()
	query.Set("client_version", codexModelsClientVersion)
	req.URL.RawQuery = query.Encode()
	if accountID == "" {
		accountID = chatgptAccountID(creds.AccessToken)
	}
	if accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openairesponses: list Codex models: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("openairesponses: read Codex models: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"openairesponses: list Codex models: %s: %s",
			res.Status,
			string(body),
		)
	}

	var wire codexModelsResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("openairesponses: decode Codex models: %w", err)
	}

	models := make([]ai.Model, 0, len(wire.Models))
	for _, model := range wire.Models {
		if model.Visibility != "list" || !model.SupportedInAPI {
			continue
		}

		levels := make([]ai.ThinkingLevel, 0, len(model.SupportedReasoningLevels))
		for _, level := range model.SupportedReasoningLevels {
			levels = append(levels, ai.ThinkingLevel(level.Effort))
		}

		models = append(models, ai.Model{
			ID:             model.Slug,
			Name:           model.DisplayName,
			Reasoning:      len(levels) > 0,
			ThinkingLevels: levels,
			ContextWindow:  model.ContextWindow,
		})
	}

	return models, nil
}

type codexModelsResponse struct {
	Models []struct {
		Slug                     string `json:"slug"`
		DisplayName              string `json:"display_name"`
		Visibility               string `json:"visibility"`
		SupportedInAPI           bool   `json:"supported_in_api"`
		ContextWindow            int    `json:"context_window"`
		SupportedReasoningLevels []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
	} `json:"models"`
}

// DetectOAuthEnv builds a Codex OAuth provider from OPENAI_OAUTH_TOKEN and
// the optional OPENAI_OAUTH_CLIENT_ID. It reports whether OPENAI_OAUTH_TOKEN
// is set.
func DetectOAuthEnv() (*Provider, bool) {
	token := os.Getenv("OPENAI_OAUTH_TOKEN")
	if token == "" {
		return nil, false
	}
	clientID := os.Getenv("OPENAI_OAUTH_CLIENT_ID")
	return NewForCodexOAuth(clientID, "", oauth.Credentials{AccessToken: token}), true
}

// DetectCodexCLI builds a provider from an existing Codex CLI login and
// reports whether it found one. It reads the $CODEX_HOME/auth.json store of
// the CLI. On refresh it reads that file again. It does not run its own
// OAuth rotation.
func DetectCodexCLI() (*Provider, bool) {
	src := codexCLISource{}
	creds, err := src.load()
	if err != nil {
		return nil, false
	}
	accountID, _ := creds.Extras[chatgptAccountIDExtra].(string)
	return NewForCodexOAuth("", accountID, creds, oauth.WithRefresher(codexReReadRefresher(src))), true
}

// codexCLISource reads the OAuth credentials of the Codex CLI from
// $CODEX_HOME/auth.json (~/.codex/auth.json by default).
type codexCLISource struct {
	// path replaces the auth.json location. Tests use it.
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

// parseCodexCreds maps the auth.json file of the Codex CLI to
// [oauth.Credentials]. The schema is
// {"tokens":{"access_token","refresh_token","account_id","id_token"}}.
// Codex holds no explicit expiry. The expiry comes from the JWT "exp" claim
// of the access token. The account ID goes into Extras for the
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

// codexReReadRefresher returns an [oauth.TokenRefresher] that reads the
// credentials again from the store of the Codex CLI. It does not run an HTTP
// refresh. If the new token is also expired, it returns an error that asks
// for a new login.
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

// chatgptAccountID extracts the ChatGPT account ID from an OpenAI OAuth
// access token. The token is a JWT. The payload holds the ID under the
// "https://api.openai.com/auth" claim. If the token is not a well-formed
// JWT, it returns "". It also returns "" when the claim is absent.
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

// jwtExpiry returns the expiry from the "exp" claim of a JWT. It returns the
// zero time when the token is not a well-formed JWT. It also returns the
// zero time when the token holds no exp claim.
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

// jwtPayload decodes the payload segment of a JWT. It returns an error when
// the token does not have three segments. It also returns an error when the
// payload is not valid base64url.
func jwtPayload(token string) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("openairesponses: not a JWT")
	}
	return base64.RawURLEncoding.DecodeString(parts[1])
}
