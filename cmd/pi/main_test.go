package main

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	codexagent "github.com/sonnes/pi-go/pkg/agent/codex"
	cursoragent "github.com/sonnes/pi-go/pkg/agent/cursor"
	"github.com/sonnes/pi-go/pkg/ai"
)

// jwtWithAuthClaim builds an unsigned JWT-shaped token whose payload carries
// the given OpenAI auth claim, for testing account-id extraction.
func jwtWithAuthClaim(t *testing.T, payloadJSON string) string {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	return "header." + enc + ".signature"
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
		{
			name:  "non-jwt yields empty",
			token: "not-a-jwt",
			want:  "",
		},
		{
			name:  "empty token yields empty",
			token: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, chatgptAccountID(tt.token))
		})
	}
}

// authProviderCreate returns the create func for the named entry in
// authProviderOrder, or nil if absent.
func authProviderCreate(name string) func(StoredCredential) (ai.Provider, error) {
	for _, e := range authProviderOrder {
		if e.name == name {
			return e.create
		}
	}
	return nil
}

// TestAuthProviderOrder_OpenAIUsesResponsesAPI verifies the OpenAI OAuth
// path builds a Responses-API provider. ChatGPT/Codex OAuth tokens are only
// honored on the Responses backend, not Chat Completions.
func TestAuthProviderOrder_OpenAIUsesResponsesAPI(t *testing.T) {
	create := authProviderCreate("openai")
	require.NotNil(t, create)

	sc := StoredCredential{
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
		ClientID:     "app_test",
	}

	p, err := create(sc)
	require.NoError(t, err)
	assert.Equal(t, "openai-responses", p.API())
}

func TestParseServerTools_Empty(t *testing.T) {
	tools, err := parseServerTools("")
	require.NoError(t, err)
	assert.Nil(t, tools)
}

func TestParseServerTools_KnownNames(t *testing.T) {
	tools, err := parseServerTools("web_search,code_execution")
	require.NoError(t, err)
	require.Len(t, tools, 2)

	assert.Equal(t, ai.ToolKindServer, tools[0].Info().Kind)
	assert.Equal(t, ai.ServerToolWebSearch, tools[0].Info().ServerType)

	assert.Equal(t, ai.ToolKindServer, tools[1].Info().Kind)
	assert.Equal(t, ai.ServerToolCodeExecution, tools[1].Info().ServerType)
}

func TestParseServerTools_TrimsWhitespaceAndSkipsEmpties(t *testing.T) {
	tools, err := parseServerTools(" web_search , , code_execution ")
	require.NoError(t, err)
	require.Len(t, tools, 2)
	assert.Equal(t, ai.ServerToolWebSearch, tools[0].Info().ServerType)
	assert.Equal(t, ai.ServerToolCodeExecution, tools[1].Info().ServerType)
}

func TestParseServerTools_UnknownName(t *testing.T) {
	tools, err := parseServerTools("web_search,frobulate")
	assert.Nil(t, tools)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown server tool "frobulate"`)
}

func TestParseServerTools_AllRecognizedTypes(t *testing.T) {
	spec := "web_search,code_execution,web_fetch,file_search,computer,bash,text_editor,tool_search,mcp"
	tools, err := parseServerTools(spec)
	require.NoError(t, err)
	require.Len(t, tools, 9)

	want := []ai.ServerToolType{
		ai.ServerToolWebSearch,
		ai.ServerToolCodeExecution,
		ai.ServerToolWebFetch,
		ai.ServerToolFileSearch,
		ai.ServerToolComputer,
		ai.ServerToolBash,
		ai.ServerToolTextEditor,
		ai.ServerToolToolSearch,
		ai.ServerToolMCP,
	}
	for i, w := range want {
		assert.Equal(t, w, tools[i].Info().ServerType, "index %d", i)
	}
}

func TestCreateAgent_CodexMode(t *testing.T) {
	a, err := createAgent("codex", "gpt-5.4", 0, "", "", "")
	require.NoError(t, err)
	defer a.Close()

	_, ok := a.(*codexagent.Agent)
	assert.True(t, ok)
}

func TestCreateAgent_CursorMode(t *testing.T) {
	a, err := createAgent("cursor", "gpt-5", 0, "", "", "")
	require.NoError(t, err)
	defer a.Close()

	_, ok := a.(*cursoragent.Agent)
	assert.True(t, ok)
}

func TestCreateAPIAgent_CodexCLIPrefix(t *testing.T) {
	a, err := createAPIAgent("codex-cli/gpt-5.4", 0, "", "")
	require.NoError(t, err)
	defer a.Close()
	defer ai.UnregisterProvider("codex-cli")

	p, ok := ai.GetProvider("codex-cli")
	require.True(t, ok)
	assert.Equal(t, "codex-cli", p.API())
}

func TestCreateAPIAgent_CursorCLIPrefix(t *testing.T) {
	a, err := createAPIAgent("cursor-cli/gpt-5", 0, "", "")
	require.NoError(t, err)
	defer a.Close()
	defer ai.UnregisterProvider("cursor-cli")

	p, ok := ai.GetProvider("cursor-cli")
	require.True(t, ok)
	assert.Equal(t, "cursor-cli", p.API())
}
