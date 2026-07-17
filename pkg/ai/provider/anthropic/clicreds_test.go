package anthropic

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
