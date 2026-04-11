package geminicli

import "github.com/sonnes/pi-go/pkg/ai/oauth"

// AuthorizeEndpoint is the Google OAuth authorization endpoint.
const AuthorizeEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"

// LoginConfig returns an [oauth.LoginConfig] for Google's Gemini CLI OAuth flow.
// The caller must set DisplayURL before passing to [oauth.Login].
func LoginConfig(clientID, clientSecret string) oauth.LoginConfig {
	return oauth.LoginConfig{
		AuthorizeURL: AuthorizeEndpoint,
		TokenURL:     TokenEndpoint,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectPort: 8085,
		RedirectPath: "/oauth2callback",
		Scopes: []string{
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		ExtraParams: map[string]string{
			"access_type": "offline",
			"prompt":      "consent",
		},
	}
}
