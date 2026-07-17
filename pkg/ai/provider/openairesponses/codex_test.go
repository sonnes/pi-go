package openairesponses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		`{"tokens":{"access_token":%q,"refresh_token":"ref","account_id":"acct-1"}}`, token,
	)), 0o600))

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
	assert.Equal(t, "openai-responses", p.Provider())
}

// TestCodexReReadRefresher_ReReads verifies the refresher returns the freshest
// credentials from the source rather than running an HTTP refresh.
func TestCodexReReadRefresher_ReReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	freshToken := jwtWithExp(t, time.Now().Add(time.Hour).Unix())
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		`{"tokens":{"access_token":%q,"refresh_token":"ref"}}`, freshToken,
	)), 0o600))

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
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		`{"tokens":{"access_token":%q,"refresh_token":"ref"}}`, expiredToken,
	)), 0o600))

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
