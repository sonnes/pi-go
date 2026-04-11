package openai

import "github.com/sonnes/pi-go/pkg/ai/oauth"

// AuthorizeEndpoint is the OpenAI OAuth authorization endpoint.
const AuthorizeEndpoint = "https://auth.openai.com/oauth/authorize"

// LoginConfig returns an [oauth.LoginConfig] for OpenAI's OAuth flow.
// The caller must set DisplayURL before passing to [oauth.Login].
func LoginConfig(clientID string) oauth.LoginConfig {
	return oauth.LoginConfig{
		AuthorizeURL: AuthorizeEndpoint,
		TokenURL:     TokenEndpoint,
		ClientID:     clientID,
		RedirectPort: 1455,
		RedirectPath: "/auth/callback",
		Scopes: []string{
			"openid",
			"profile",
			"email",
			"offline_access",
		},
	}
}
