package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sonnes/pi-go/pkg/ai/oauth"
)

// StoredCredential is the JSON-serializable form of [oauth.Credentials]
// plus the client credentials needed for token refresh.
type StoredCredential struct {
	AccessToken  string         `json:"access_token"`
	RefreshToken string         `json:"refresh_token"`
	ExpiresAt    time.Time      `json:"expires_at"`
	ClientID     string         `json:"client_id"`
	ClientSecret string         `json:"client_secret,omitempty"`
	Extras       map[string]any `json:"extras,omitempty"`
}

// StoredCredentials maps provider names to their OAuth credentials.
type StoredCredentials map[string]StoredCredential

// ToOAuthCredentials converts a StoredCredential to [oauth.Credentials].
func (s StoredCredential) ToOAuthCredentials() oauth.Credentials {
	return oauth.Credentials{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		ExpiresAt:    s.ExpiresAt,
		Extras:       s.Extras,
	}
}

// FromOAuthCredentials creates a StoredCredential from [oauth.Credentials]
// and the client ID/secret used during login.
func FromOAuthCredentials(
	creds oauth.Credentials,
	clientID string,
	clientSecret string,
) StoredCredential {
	return StoredCredential{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		ExpiresAt:    creds.ExpiresAt,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Extras:       creds.Extras,
	}
}

// authFilePath returns the path to the credentials file (~/.pigo/auth.json).
func authFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("auth: home dir: %w", err)
	}
	return filepath.Join(home, ".pigo", "auth.json"), nil
}

// LoadAuth reads credentials from the default auth file.
// Returns an empty map if the file does not exist.
func LoadAuth() (StoredCredentials, error) {
	path, err := authFilePath()
	if err != nil {
		return nil, err
	}
	return LoadAuthFrom(path)
}

// SaveAuth writes credentials to the default auth file with mode 0600.
func SaveAuth(creds StoredCredentials) error {
	path, err := authFilePath()
	if err != nil {
		return err
	}
	return SaveAuthTo(path, creds)
}

// LoadAuthFrom reads credentials from the given path.
// Returns an empty map if the file does not exist.
func LoadAuthFrom(path string) (StoredCredentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StoredCredentials{}, nil
		}
		return nil, fmt.Errorf("auth: read %s: %w", path, err)
	}

	var creds StoredCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("auth: decode %s: %w", path, err)
	}
	return creds, nil
}

// SaveAuthTo writes credentials to the given path with mode 0600.
// It creates parent directories as needed and writes atomically.
func SaveAuthTo(path string, creds StoredCredentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("auth: create dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("auth: encode: %w", err)
	}

	// Atomic write: temp file then rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("auth: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("auth: rename %s: %w", path, err)
	}

	return nil
}
