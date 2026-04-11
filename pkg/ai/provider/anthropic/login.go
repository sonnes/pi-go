package anthropic

import "github.com/sonnes/pi-go/pkg/ai/oauth"

// AuthorizeEndpoint is the Anthropic OAuth authorization endpoint.
const AuthorizeEndpoint = "https://claude.ai/oauth/authorize"

// LoginConfig returns an [oauth.LoginConfig] for Anthropic's OAuth flow.
// The caller must set DisplayURL before passing to [oauth.Login].
func LoginConfig(clientID string) oauth.LoginConfig {
	return oauth.LoginConfig{
		AuthorizeURL:                AuthorizeEndpoint,
		TokenURL:                    TokenEndpoint,
		ClientID:                    clientID,
		RedirectPort:                53692,
		UseJSONTokenRequest:         true,
		IncludeStateInTokenExchange: true,
		Scopes: []string{
			"org:create_api_key",
			"user:profile",
			"user:inference",
			"user:sessions:claude_code",
			"user:mcp_servers",
			"user:file_upload",
		},
	}
}
