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

// This file implements the "reuse the Claude Code login" credential tier: pick
// up an existing Claude Pro/Max subscription login that the official Claude CLI
// already obtained, with zero configuration.
//
// We re-read the CLI's own credential store rather than running our own OAuth
// refresh, so we need no client ID and never rotate (and thus never invalidate)
// the CLI's refresh token. See claudeReReadRefresher.

// claudeKeychainService is the macOS Keychain service name under which Claude
// Code stores its OAuth credentials. The account is the local macOS username,
// so the lookup matches by service only.
const claudeKeychainService = "Claude Code-credentials"

// DetectClaudeCLI builds a provider from an existing Claude Code login and
// reports whether one was found. It reads the CLI's own credential store (the
// macOS login Keychain, then ~/.claude/.credentials.json) and re-reads it on
// refresh rather than running its own OAuth rotation.
func DetectClaudeCLI() (*Provider, bool) {
	src := claudeCLISource{}
	creds, err := src.load()
	if err != nil {
		return nil, false
	}
	// Empty clientID is intentional: the re-read refresher replaces the
	// default HTTP refresher, so no client ID is needed.
	return New(WithOAuth("", creds, oauth.WithRefresher(claudeReReadRefresher(src)))), true
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
		return nil, fmt.Errorf("anthropic: read claude keychain: %w", err)
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

// claudeReReadRefresher returns a [oauth.TokenRefresher] that re-reads
// credentials from Claude Code's own store. The CLI refreshes its tokens in the
// background; we pick up the latest rather than running our own refresh. If the
// re-read token is also expired, it returns an error directing the user to
// re-authenticate with the CLI.
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
