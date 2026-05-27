package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginConfigMatchesClaudeCodeOAuth(t *testing.T) {
	cfg := LoginConfig("test-client-id")

	assert.Equal(t, "https://claude.com/cai/oauth/authorize", cfg.AuthorizeURL)
	assert.Equal(t, TokenEndpoint, cfg.TokenURL)
	assert.Equal(t, "test-client-id", cfg.ClientID)
	assert.Equal(t, 53692, cfg.RedirectPort)
	assert.True(t, cfg.UseJSONTokenRequest)
	assert.True(t, cfg.IncludeStateInTokenExchange)
	assert.Equal(t, "true", cfg.ExtraParams["code"])

	require.Contains(t, cfg.Scopes, "org:create_api_key")
	require.Contains(t, cfg.Scopes, "user:profile")
	require.Contains(t, cfg.Scopes, "user:inference")
	require.Contains(t, cfg.Scopes, "user:sessions:claude_code")
	require.Contains(t, cfg.Scopes, "user:mcp_servers")
	require.Contains(t, cfg.Scopes, "user:file_upload")
}
