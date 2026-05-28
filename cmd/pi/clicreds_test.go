package main

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

func TestParseClaudeCreds(t *testing.T) {
	expires := time.Now().Add(time.Hour).UnixMilli()
	data := fmt.Sprintf(
		`{"claudeAiOauth":{"accessToken":"acc","refreshToken":"ref","expiresAt":%d}}`,
		expires,
	)

	creds, err := parseClaudeCreds([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "acc", creds.AccessToken)
	assert.Equal(t, "ref", creds.RefreshToken)
	assert.Equal(t, time.UnixMilli(expires), creds.ExpiresAt)
	assert.False(t, creds.IsExpired())
}

func TestParseClaudeCreds_Errors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed json", data: `{`},
		{name: "missing access token", data: `{"claudeAiOauth":{"refreshToken":"ref"}}`},
		{name: "empty object", data: `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseClaudeCreds([]byte(tt.data))
			require.Error(t, err)
		})
	}
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

func TestClaudeCLISource_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	expires := time.Now().Add(time.Hour).UnixMilli()
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		`{"claudeAiOauth":{"accessToken":"acc","refreshToken":"ref","expiresAt":%d}}`, expires,
	)), 0o600))

	// A non-empty path skips the Keychain even on macOS.
	src := claudeCLISource{path: path}
	creds, err := src.load()
	require.NoError(t, err)
	assert.Equal(t, "acc", creds.AccessToken)
}

// TestCLICredRefresher_ReReads verifies the refresher returns the freshest
// credentials from the source rather than running an HTTP refresh.
func TestCLICredRefresher_ReReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	freshToken := jwtWithExp(t, time.Now().Add(time.Hour).Unix())
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		`{"tokens":{"access_token":%q,"refresh_token":"ref"}}`, freshToken,
	)), 0o600))

	refresher := cliCredRefresher(codexCLISource{path: path})
	got, err := refresher.RefreshToken(t.Context(), oauth.Credentials{AccessToken: "stale"})
	require.NoError(t, err)
	assert.Equal(t, freshToken, got.AccessToken)
}

// TestCLICredRefresher_ExpiredError verifies that when the re-read token is
// still expired, the refresher surfaces a re-auth error instead of returning
// stale credentials.
func TestCLICredRefresher_ExpiredError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	expiredToken := jwtWithExp(t, time.Now().Add(-time.Hour).Unix())
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		`{"tokens":{"access_token":%q,"refresh_token":"ref"}}`, expiredToken,
	)), 0o600))

	refresher := cliCredRefresher(codexCLISource{path: path})
	_, err := refresher.RefreshToken(t.Context(), oauth.Credentials{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-authenticate")
}

// TestCLICredRefresher_MissingSourceError verifies an absent source surfaces
// a reload error.
func TestCLICredRefresher_MissingSourceError(t *testing.T) {
	refresher := cliCredRefresher(codexCLISource{path: filepath.Join(t.TempDir(), "absent.json")})
	_, err := refresher.RefreshToken(t.Context(), oauth.Credentials{})
	require.Error(t, err)
}
