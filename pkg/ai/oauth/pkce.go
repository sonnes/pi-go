package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCE holds a PKCE verifier and its corresponding S256 challenge.
type PKCE struct {
	Verifier  string
	Challenge string
}

// GeneratePKCE creates a new PKCE verifier (base64url of 32 random bytes)
// and its SHA-256 challenge for use in OAuth authorization code flows.
func GeneratePKCE() (PKCE, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return PKCE{}, fmt.Errorf("oauth: generate PKCE verifier: %w", err)
	}

	verifier := base64.RawURLEncoding.EncodeToString(b)

	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	return PKCE{
		Verifier:  verifier,
		Challenge: challenge,
	}, nil
}
