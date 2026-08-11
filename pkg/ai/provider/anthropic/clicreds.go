package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sonnes/pi-go/pkg/ai/oauth"
)

// This file implements the "reuse the Claude Code login" credential tier. It
// uses an existing Claude Pro or Max subscription login that the official
// Claude CLI already obtained. This tier needs no configuration.
//
// This file reads the credential store of the CLI again instead of running its
// own OAuth refresh. As a result, it needs no client ID. It also never rotates
// the refresh token of the CLI, so it never invalidates that token. See
// claudeReReadRefresher.

// claudeKeychainService is the macOS Keychain service name under which Claude
// Code stores its OAuth credentials. The account is the local macOS username,
// so the lookup matches by service only.
const claudeKeychainService = "Claude Code-credentials"

// DetectClaudeCLI builds a provider from an existing Claude Code login and
// reports whether it found one. It reads the credential store of the CLI: the
// macOS login Keychain first, then ~/.claude/.credentials.json. On refresh, it
// reads that store again instead of running its own OAuth rotation.
func DetectClaudeCLI() (*Provider, bool) {
	src := claudeCLISource{}
	creds, err := src.load()
	if err != nil {
		return nil, false
	}
	// The empty clientID is intentional. The re-read refresher replaces the
	// default HTTP refresher, so the provider needs no client ID.
	return New(WithOAuth("", creds, oauth.WithRefresher(claudeReReadRefresher(src)))), true
}

// claudeCLISource reads the OAuth credentials of Claude Code. On macOS it
// tries the login Keychain first. If that read fails, it uses
// ~/.claude/.credentials.json.
type claudeCLISource struct {
	// path overrides the credentials file (for tests). If path is set, the
	// source does not read the Keychain.
	path string
	// readKeychain overrides Keychain access (for tests).
	readKeychain func() ([]byte, error)
}

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
		// Continue to the file on disk.
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

// readClaudeKeychain reads the credential blob of Claude Code from the macOS
// login Keychain.
func readClaudeKeychain() ([]byte, error) {
	out, err := exec.Command(
		"security",
		"find-generic-password",
		"-s", claudeKeychainService,
		"-w",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("anthropic: read claude keychain: %w", err)
	}
	return bytes.TrimSpace(out), nil
}

// parseClaudeCreds maps the credential JSON of Claude Code to
// [oauth.Credentials]. The schema is
// {"claudeAiOauth":{"accessToken","refreshToken","expiresAt"}}. The expiresAt
// field holds Unix milliseconds.
func parseClaudeCreds(data []byte) (oauth.Credentials, error) {
	var doc struct {
		ClaudeAIOAuth struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return oauth.Credentials{}, fmt.Errorf("anthropic: parse claude credentials: %w", err)
	}

	o := doc.ClaudeAIOAuth
	if o.AccessToken == "" {
		return oauth.Credentials{}, fmt.Errorf("anthropic: no claude access token found")
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

// claudeReReadRefresher returns an [oauth.TokenRefresher] that reads the
// credentials from the store of Claude Code again. The CLI refreshes its tokens
// in the background. The refresher takes the latest tokens instead of running
// its own refresh. If the new token is also expired, the refresher returns an
// error that tells the user to authenticate again with the CLI.
func claudeReReadRefresher(src claudeCLISource) oauth.TokenRefresher {
	return oauth.TokenRefresherFunc(func(_ context.Context, _ oauth.Credentials) (oauth.Credentials, error) {
		creds, err := src.load()
		if err != nil {
			return oauth.Credentials{}, fmt.Errorf("anthropic: reload Claude Code credentials: %w", err)
		}
		if creds.IsExpired() {
			return oauth.Credentials{}, fmt.Errorf(
				"anthropic: Claude Code credentials are expired; re-authenticate with the Claude CLI",
			)
		}
		return creds, nil
	})
}
