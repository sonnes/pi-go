package openairesponses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/oauth"
)

// jwtWithExp builds an unsigned JWT-shaped token carrying the given exp claim.
func jwtWithExp(t *testing.T, exp int64) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"exp": exp})
	require.NoError(t, err)
	enc := base64.RawURLEncoding.EncodeToString(payload)
	return "header." + enc + ".signature"
}

// jwtWithAuthClaim builds an unsigned JWT-shaped token whose payload carries
// the given OpenAI auth claim, for testing account-id extraction.
func jwtWithAuthClaim(t *testing.T, payloadJSON string) string {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	return "header." + enc + ".signature"
}

func TestParseCodexCreds(t *testing.T) {
	exp := time.Now().Add(time.Hour).Unix()
	token := jwtWithExp(t, exp)
	data := fmt.Sprintf(
		`{"tokens":{"access_token":%q,"refresh_token":"ref","account_id":"acct-9","id_token":"id"},"last_refresh":"2026-05-27"}`,
		token,
	)

	creds, err := parseCodexCreds([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, token, creds.AccessToken)
	assert.Equal(t, "ref", creds.RefreshToken)
	assert.Equal(t, time.Unix(exp, 0), creds.ExpiresAt)
	assert.Equal(t, "acct-9", creds.Extras[chatgptAccountIDExtra])
}

func TestParseCodexCreds_NoAccountID(t *testing.T) {
	data := `{"tokens":{"access_token":"acc","refresh_token":"ref"}}`
	creds, err := parseCodexCreds([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "acc", creds.AccessToken)
	assert.Nil(t, creds.Extras)
}

func TestParseCodexCreds_MissingAccessToken(t *testing.T) {
	_, err := parseCodexCreds([]byte(`{"tokens":{"refresh_token":"ref"}}`))
	require.Error(t, err)
}

func TestJWTExpiry(t *testing.T) {
	exp := time.Now().Add(2 * time.Hour).Unix()
	assert.Equal(t, time.Unix(exp, 0), jwtExpiry(jwtWithExp(t, exp)))

	// Non-JWT and no-exp tokens yield the zero time.
	assert.True(t, jwtExpiry("not-a-jwt").IsZero())
	assert.True(t, jwtExpiry("header.bad-base64!.sig").IsZero())
}

func TestChatGPTAccountID(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "extracts account id from auth claim",
			token: jwtWithAuthClaim(t, `{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-123"}}`),
			want:  "acct-123",
		},
		{
			name:  "missing claim yields empty",
			token: jwtWithAuthClaim(t, `{"sub":"user"}`),
			want:  "",
		},
		{name: "non-jwt yields empty", token: "not-a-jwt", want: ""},
		{name: "empty token yields empty", token: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, chatgptAccountID(tt.token))
		})
	}
}

func TestCodexCLISource_Load(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	token := jwtWithExp(t, time.Now().Add(time.Hour).Unix())
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil,
		`{"tokens":{"access_token":%q,"refresh_token":"ref","account_id":"acct-1"}}`, token,
	), 0o600))

	src := codexCLISource{path: path}
	creds, err := src.load()
	require.NoError(t, err)
	assert.Equal(t, token, creds.AccessToken)
	assert.Equal(t, "acct-1", creds.Extras[chatgptAccountIDExtra])
}

// TestNewForCodexOAuth_UsesResponsesAPI verifies the ChatGPT/Codex OAuth path
// builds a Responses-API provider. These tokens are honored only on the
// Responses backend, not Chat Completions.
func TestNewForCodexOAuth_UsesResponsesAPI(t *testing.T) {
	p := NewForCodexOAuth("app_test", "", oauth.Credentials{AccessToken: "test-token"})
	require.NotNil(t, p)
	assert.Equal(t, "openai-responses", ID)
}

func TestListCodexModels(t *testing.T) {
	var gotURL, gotAuth, gotAccount string
	base := codexRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		gotURL = req.URL.String()
		gotAuth = req.Header.Get("Authorization")
		gotAccount = req.Header.Get("chatgpt-account-id")

		body := `{"models":[
			{"slug":"gpt-visible","display_name":"GPT Visible","visibility":"list","supported_in_api":true,"context_window":272000,"supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}]},
			{"slug":"gpt-hidden","display_name":"GPT Hidden","visibility":"hide","supported_in_api":true,"supported_reasoning_levels":[]},
			{"slug":"gpt-ui-only","display_name":"GPT UI Only","visibility":"list","supported_in_api":false,"supported_reasoning_levels":[]}
		]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	models, err := ListCodexModels(
		t.Context(),
		"client-id",
		"account-id",
		oauth.Credentials{
			AccessToken: "access-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
		oauth.WithBase(base),
	)
	require.NoError(t, err)

	assert.Equal(t, openAICodexBaseURL+"/models?client_version=0.0.0", gotURL)
	assert.Equal(t, "Bearer access-token", gotAuth)
	assert.Equal(t, "account-id", gotAccount)
	require.Len(t, models, 1)
	assert.Equal(t, "gpt-visible", models[0].ID)
	assert.Equal(t, "GPT Visible", models[0].Name)
	assert.Equal(t, 272000, models[0].ContextWindow)
	assert.True(t, models[0].Reasoning)
	assert.Equal(t, []ai.ThinkingLevel{ai.ThinkingLow, ai.ThinkingHigh}, models[0].ThinkingLevels)
}

type codexRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f codexRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestCodexReReadRefresher_ReReads verifies the refresher returns the freshest
// credentials from the source rather than running an HTTP refresh.
func TestCodexReReadRefresher_ReReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	freshToken := jwtWithExp(t, time.Now().Add(time.Hour).Unix())
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil,
		`{"tokens":{"access_token":%q,"refresh_token":"ref"}}`, freshToken,
	), 0o600))

	refresher := codexReReadRefresher(codexCLISource{path: path})
	got, err := refresher.RefreshToken(t.Context(), oauth.Credentials{AccessToken: "stale"})
	require.NoError(t, err)
	assert.Equal(t, freshToken, got.AccessToken)
}

// TestCodexReReadRefresher_ExpiredError verifies that when the re-read token is
// still expired, the refresher surfaces a re-auth error.
func TestCodexReReadRefresher_ExpiredError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	expiredToken := jwtWithExp(t, time.Now().Add(-time.Hour).Unix())
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil,
		`{"tokens":{"access_token":%q,"refresh_token":"ref"}}`, expiredToken,
	), 0o600))

	refresher := codexReReadRefresher(codexCLISource{path: path})
	_, err := refresher.RefreshToken(t.Context(), oauth.Credentials{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-authenticate")
}

// TestCodexReReadRefresher_MissingSourceError verifies an absent source
// surfaces a reload error.
func TestCodexReReadRefresher_MissingSourceError(t *testing.T) {
	refresher := codexReReadRefresher(codexCLISource{path: filepath.Join(t.TempDir(), "absent.json")})
	_, err := refresher.RefreshToken(t.Context(), oauth.Credentials{})
	require.Error(t, err)
}
