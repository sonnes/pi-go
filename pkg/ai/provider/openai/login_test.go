package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoginConfigMatchesCodexBrowserFlow(t *testing.T) {
	cfg := LoginConfig("client-id")

	assert.Equal(t, "client-id", cfg.ClientID)
	assert.Contains(t, cfg.Scopes, "api.connectors.read")
	assert.Contains(t, cfg.Scopes, "api.connectors.invoke")
	assert.Equal(t, "true", cfg.ExtraParams["id_token_add_organizations"])
	assert.Equal(t, "true", cfg.ExtraParams["codex_cli_simplified_flow"])
	assert.NotEmpty(t, cfg.ExtraParams["originator"])
}
