package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
)

// This file implements the "reuse official CLI logins" credential tier: pi
// picks up an existing Claude Pro/Max or ChatGPT subscription login that the
// official CLI (Claude Code, Codex) already obtained, with zero configuration.
//
// We re-read the CLI's own credential store rather than running our own OAuth
// refresh. That means we need no client ID at all, and we never rotate (and
// thus never invalidate) the CLI's refresh token — mirroring openclaw's
// "token sink" contract. See cliCredRefresher.

// chatgptAccountIDExtra is the [oauth.Credentials.Extras] key under which the
// reuse tier stashes the ChatGPT account ID read from the Codex CLI, so it can
// survive token re-reads and be applied as the chatgpt-account-id header.
const chatgptAccountIDExtra = "chatgpt_account_id"

// claudeKeychainService is the macOS Keychain service name under which Claude
// Code stores its OAuth credentials. The account is the local macOS username,
// so the lookup matches by service only.
const claudeKeychainService = "Claude Code-credentials"

// cliCredSource loads OAuth credentials that an official provider CLI has
// already obtained and keeps refreshed.
type cliCredSource interface {
	// label names the source for diagnostics, e.g. "Claude Code CLI".
	label() string
	// load reads the freshest credentials from the CLI's own store. It
	// returns an error if the store is absent or unparseable.
	load() (oauth.Credentials, error)
}

// claudeCLISource reads Claude Code's OAuth credentials. On macOS it tries the
// login Keychain first, then falls back to ~/.claude/.credentials.json.
type claudeCLISource struct {
	// path overrides the credentials file (for tests). When set, the
	// Keychain is skipped.
	path string
	// readKeychain overrides Keychain access (for tests).
	readKeychain func() ([]byte, error)
}

func (s claudeCLISource) label() string { return "Claude Code CLI" }

func (s claudeCLISource) load() (oauth.Credentials, error) {
	data, err := s.read()
	if err != nil {
		return oauth.Credentials{}, err
	}
	return parseClaudeCreds(data)
}

func (s claudeCLISource) read() ([]byte, error) {
	if s.path == "" && runtime.GOOS == "darwin" {
		read := s.readKeychain
		if read == nil {
			read = readClaudeKeychain
		}
		if data, err := read(); err == nil {
			return data, nil
		}
		// Fall through to the on-disk file.
	}

	path := s.path
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".claude", ".credentials.json")
	}
	return os.ReadFile(path)
}

// readClaudeKeychain reads Claude Code's credential blob from the macOS
// login Keychain.
func readClaudeKeychain() ([]byte, error) {
	out, err := exec.Command(
		"security",
		"find-generic-password",
		"-s", claudeKeychainService,
		"-w",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("clicreds: read claude keychain: %w", err)
	}
	return bytes.TrimSpace(out), nil
}

// parseClaudeCreds maps Claude Code's credential JSON to [oauth.Credentials].
// The schema is {"claudeAiOauth":{"accessToken","refreshToken","expiresAt"}}
// where expiresAt is Unix milliseconds.
func parseClaudeCreds(data []byte) (oauth.Credentials, error) {
	var doc struct {
		ClaudeAIOAuth struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return oauth.Credentials{}, fmt.Errorf("clicreds: parse claude credentials: %w", err)
	}

	o := doc.ClaudeAIOAuth
	if o.AccessToken == "" {
		return oauth.Credentials{}, fmt.Errorf("clicreds: no claude access token found")
	}

	creds := oauth.Credentials{
		AccessToken:  o.AccessToken,
		RefreshToken: o.RefreshToken,
	}
	if o.ExpiresAt > 0 {
		creds.ExpiresAt = time.UnixMilli(o.ExpiresAt)
	}
	return creds, nil
}

// codexCLISource reads the Codex CLI's OAuth credentials from
// $CODEX_HOME/auth.json (default ~/.codex/auth.json).
type codexCLISource struct {
	// path overrides the auth.json location (for tests).
	path string
}

func (s codexCLISource) label() string { return "Codex CLI" }

func (s codexCLISource) load() (oauth.Credentials, error) {
	path := s.path
	if path == "" {
		var err error
		if path, err = codexAuthPath(); err != nil {
			return oauth.Credentials{}, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return oauth.Credentials{}, err
	}
	return parseCodexCreds(data)
}

func codexAuthPath() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

// parseCodexCreds maps the Codex CLI's auth.json to [oauth.Credentials]. The
// schema is {"tokens":{"access_token","refresh_token","account_id","id_token"}}.
// Codex carries no explicit expiry, so it is derived from the access token's
// JWT "exp" claim. The account ID is stashed in Extras for the
// chatgpt-account-id header.
func parseCodexCreds(data []byte) (oauth.Credentials, error) {
	var doc struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
			IDToken      string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return oauth.Credentials{}, fmt.Errorf("clicreds: parse codex auth: %w", err)
	}

	t := doc.Tokens
	if t.AccessToken == "" {
		return oauth.Credentials{}, fmt.Errorf("clicreds: no codex access token found")
	}

	creds := oauth.Credentials{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    jwtExpiry(t.AccessToken),
	}
	if t.AccountID != "" {
		creds.Extras = map[string]any{chatgptAccountIDExtra: t.AccountID}
	}
	return creds, nil
}

// cliCredRefresher returns a [oauth.TokenRefresher] that re-reads credentials
// from the official CLI's own store. The CLI refreshes its tokens in the
// background; we pick up the latest rather than running our own refresh. If
// the re-read token is also expired, it returns an error directing the user
// to re-authenticate with that CLI.
func cliCredRefresher(src cliCredSource) oauth.TokenRefresher {
	return oauth.TokenRefresherFunc(func(_ context.Context, _ oauth.Credentials) (oauth.Credentials, error) {
		creds, err := src.load()
		if err != nil {
			return oauth.Credentials{}, fmt.Errorf("clicreds: reload from %s: %w", src.label(), err)
		}
		if creds.IsExpired() {
			return oauth.Credentials{}, fmt.Errorf(
				"clicreds: %s credentials are expired; re-authenticate with that CLI",
				src.label(),
			)
		}
		return creds, nil
	})
}

// cliCredProviderOrder defines the reuse-tier providers and how to build each
// from credentials minted by its official CLI. Order mirrors authProviderOrder.
var cliCredProviderOrder = []struct {
	name   string
	source cliCredSource
	create func(src cliCredSource) (ai.Provider, error)
}{
	{
		name:   "anthropic",
		source: claudeCLISource{},
		create: func(src cliCredSource) (ai.Provider, error) {
			creds, err := src.load()
			if err != nil {
				return nil, err
			}
			// Empty clientID is intentional: the re-read refresher replaces
			// the default HTTP refresher, so no client ID is needed.
			return anthropic.New(
				anthropic.WithOAuth("", creds,
					oauth.WithBase(maybeDebugTransport(http.DefaultTransport)),
					oauth.WithRefresher(cliCredRefresher(src)),
				),
			), nil
		},
	},
	{
		name:   "openai",
		source: codexCLISource{},
		create: func(src cliCredSource) (ai.Provider, error) {
			creds, err := src.load()
			if err != nil {
				return nil, err
			}
			accountID, _ := creds.Extras[chatgptAccountIDExtra].(string)
			return newOpenAIOAuthProvider(
				"", accountID, creds,
				oauth.WithRefresher(cliCredRefresher(src)),
			), nil
		},
	},
}

// detectFromCLICreds tries to build a provider by reusing a login already
// performed by an official provider CLI. If hint is non-empty, only that
// provider is tried. Sources that are absent or unusable are skipped silently.
func detectFromCLICreds(hint string) (ai.Provider, string, error) {
	for _, entry := range cliCredProviderOrder {
		if hint != "" && entry.name != hint {
			continue
		}

		p, err := entry.create(entry.source)
		if err != nil {
			continue
		}

		fmt.Fprintf(os.Stderr, "[provider: %s via %s login]\n", entry.name, entry.source.label())
		return p, entry.name, nil
	}

	return nil, "", fmt.Errorf("no usable CLI credentials")
}

// jwtExpiry returns the expiry encoded in a JWT's "exp" claim, or the zero
// time if the token is not a well-formed JWT or carries no exp.
func jwtExpiry(token string) time.Time {
	payload, err := jwtPayload(token)
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

// jwtPayload decodes the payload segment of a JWT. It returns an error if the
// token is not a three-segment JWT or the payload is not valid base64url.
func jwtPayload(token string) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("clicreds: not a JWT")
	}
	return base64.RawURLEncoding.DecodeString(parts[1])
}
