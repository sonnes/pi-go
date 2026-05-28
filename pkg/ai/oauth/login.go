package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LoginConfig describes the OAuth endpoints and parameters for a
// provider's authorization code flow with PKCE.
type LoginConfig struct {
	// AuthorizeURL is the provider's OAuth authorization endpoint.
	AuthorizeURL string
	// TokenURL is the provider's OAuth token endpoint.
	TokenURL string
	// ClientID is the OAuth application client ID.
	ClientID string
	// ClientSecret is the OAuth client secret (required by some providers).
	ClientSecret string
	// RedirectPort is the localhost port for the callback server.
	RedirectPort int
	// RedirectPath is the path for the OAuth callback. Defaults to "/callback".
	RedirectPath string
	// Scopes is the list of OAuth scopes to request.
	Scopes []string
	// ExtraParams holds additional query parameters for the authorize URL.
	ExtraParams map[string]string
	// ExtraTokenParams holds additional parameters for the token exchange.
	ExtraTokenParams map[string]string
	// UseJSONTokenRequest sends the token exchange as JSON instead of
	// form-encoded. Some providers (e.g. Anthropic) require this.
	UseJSONTokenRequest bool
	// IncludeStateInTokenExchange includes the state parameter in the
	// token exchange request. Some providers (e.g. Anthropic) require it,
	// others (e.g. OpenAI) reject it.
	IncludeStateInTokenExchange bool
	// DisplayURL is called with the authorization URL that the user must
	// open in a browser. The application provides this callback to control
	// how the URL is presented.
	DisplayURL func(url string) error
	// ReadCode, if set, enables a manual code-paste fallback for
	// environments where the localhost callback cannot be reached (headless,
	// SSH, VPS). It is called to obtain a pasted authorization code or full
	// redirect URL. It runs concurrently with the localhost callback server;
	// whichever delivers a valid code first wins. The callback should respect
	// ctx cancellation. If the callback server cannot bind its port, login
	// proceeds with the paste path alone.
	ReadCode func(ctx context.Context) (string, error)
}

func (c LoginConfig) redirectPath() string {
	if c.RedirectPath != "" {
		return c.RedirectPath
	}
	return "/callback"
}

func (c LoginConfig) redirectURI() string {
	return fmt.Sprintf("http://localhost:%d%s", c.RedirectPort, c.redirectPath())
}

// callbackResult carries an authorization code (or error) from either the
// localhost callback server or the manual paste path.
type callbackResult struct {
	code string
	err  error
}

// Login performs an OAuth authorization code flow with PKCE.
//
// It starts a local callback server, builds the authorize URL, waits for the
// authorization code, and exchanges it for tokens. If [LoginConfig.ReadCode]
// is set, a manual code-paste path runs concurrently for environments where
// the localhost callback cannot be reached; whichever delivers a valid code
// first wins.
func Login(ctx context.Context, cfg LoginConfig) (Credentials, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return Credentials{}, err
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return Credentials{}, fmt.Errorf("oauth: generate state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	// Buffered for up to two writers (callback server + paste goroutine).
	codeCh := make(chan callbackResult, 2)

	srv, err := startCallbackServer(cfg, state, codeCh)
	if err != nil {
		// Without a paste fallback there is no way to receive the code.
		if cfg.ReadCode == nil {
			return Credentials{}, err
		}
	} else {
		defer srv.Close()
	}

	authURL, err := buildAuthorizeURL(cfg, state, pkce)
	if err != nil {
		return Credentials{}, err
	}

	if cfg.DisplayURL != nil {
		if err := cfg.DisplayURL(authURL); err != nil {
			return Credentials{}, fmt.Errorf("oauth: display URL: %w", err)
		}
	}

	if cfg.ReadCode != nil {
		go readPastedCode(ctx, cfg, state, codeCh)
	}

	// Wait for a code from either path, or context cancellation.
	select {
	case result := <-codeCh:
		if result.err != nil {
			return Credentials{}, result.err
		}
		return exchangeCode(ctx, cfg, result.code, state, pkce.Verifier)
	case <-ctx.Done():
		return Credentials{}, fmt.Errorf("oauth: login timed out waiting for callback")
	}
}

// startCallbackServer binds the localhost redirect listener and serves the
// OAuth callback, forwarding the authorization code to codeCh. The returned
// server should be closed by the caller.
func startCallbackServer(cfg LoginConfig, state string, codeCh chan<- callbackResult) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.redirectPath(), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, callbackHTML("Authentication Failed", "State mismatch. Please try again."))
			return
		}

		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, callbackHTML("Authentication Failed", desc))
			codeCh <- callbackResult{err: fmt.Errorf("oauth: provider error: %s: %s", errParam, desc)}
			return
		}

		code := r.URL.Query().Get("code")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, callbackHTML("Authentication Successful", "You can close this window."))
		codeCh <- callbackResult{code: code}
	})

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.RedirectPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("oauth: listen on %s: %w", addr, err)
	}

	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	return srv, nil
}

// readPastedCode invokes cfg.ReadCode, parses the pasted value, validates the
// state (when the paste includes one), and forwards the result to codeCh.
func readPastedCode(ctx context.Context, cfg LoginConfig, state string, codeCh chan<- callbackResult) {
	input, err := cfg.ReadCode(ctx)
	if err != nil {
		codeCh <- callbackResult{err: fmt.Errorf("oauth: read code: %w", err)}
		return
	}

	code, pastedState := parsePastedCode(input)
	if code == "" {
		codeCh <- callbackResult{err: fmt.Errorf("oauth: no authorization code found in pasted input")}
		return
	}
	if pastedState != "" && pastedState != state {
		codeCh <- callbackResult{err: fmt.Errorf("oauth: state mismatch in pasted input")}
		return
	}
	codeCh <- callbackResult{code: code}
}

// parsePastedCode extracts the authorization code and optional state from a
// value pasted by the user. It accepts a full redirect URL
// ("http://localhost:1455/callback?code=abc&state=xyz"), the "code#state"
// form some providers display inline, or a bare authorization code.
func parsePastedCode(input string) (code, state string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}

	if u, err := url.Parse(input); err == nil && u.Query().Get("code") != "" {
		q := u.Query()
		return q.Get("code"), q.Get("state")
	}

	if before, after, found := strings.Cut(input, "#"); found {
		return before, after
	}

	return input, ""
}

func buildAuthorizeURL(cfg LoginConfig, state string, pkce PKCE) (string, error) {
	u, err := url.Parse(cfg.AuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("oauth: parse authorize URL: %w", err)
	}

	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", cfg.redirectURI())
	q.Set("state", state)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")

	if len(cfg.Scopes) > 0 {
		q.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	for k, v := range cfg.ExtraParams {
		q.Set(k, v)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

func exchangeCode(
	ctx context.Context,
	cfg LoginConfig,
	code string,
	state string,
	verifier string,
) (Credentials, error) {
	params := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  cfg.redirectURI(),
		"client_id":     cfg.ClientID,
		"code_verifier": verifier,
	}
	if cfg.ClientSecret != "" {
		params["client_secret"] = cfg.ClientSecret
	}
	for k, v := range cfg.ExtraTokenParams {
		params[k] = v
	}
	if cfg.IncludeStateInTokenExchange && state != "" {
		params["state"] = state
	}

	var bodyReader *strings.Reader
	var contentType string

	if cfg.UseJSONTokenRequest {
		jsonBytes, err := json.Marshal(params)
		if err != nil {
			return Credentials{}, fmt.Errorf("oauth: marshal token request: %w", err)
		}
		bodyReader = strings.NewReader(string(jsonBytes))
		contentType = "application/json"
	} else {
		form := url.Values{}
		for k, v := range params {
			form.Set(k, v)
		}
		bodyReader = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		cfg.TokenURL,
		bodyReader,
	)
	if err != nil {
		return Credentials{}, fmt.Errorf("oauth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("oauth: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Credentials{}, fmt.Errorf("oauth: read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf(
			"oauth: token exchange failed with status %d: %s",
			resp.StatusCode,
			string(body),
		)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return Credentials{}, fmt.Errorf("oauth: decode token response: %w", err)
	}

	return Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

func callbackHTML(title, message string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>%s</title>
<style>body{font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0}
.card{text-align:center;padding:2rem;border-radius:12px;background:#16213e}</style>
</head><body><div class="card"><h1>%s</h1><p>%s</p></div></body></html>`,
		title, title, message)
}
