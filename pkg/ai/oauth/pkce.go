package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCE holds a PKCE verifier and the S256 challenge for that verifier.
type PKCE struct {
	Verifier  string
	Challenge string
}

// GeneratePKCE creates a new PKCE verifier and its SHA-256 challenge. The
// verifier is the base64url form of 32 random bytes. OAuth authorization code
// flows use both values.
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
