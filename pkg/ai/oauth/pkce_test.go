package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePKCE(t *testing.T) {
	p, err := GeneratePKCE()
	require.NoError(t, err)

	assert.NotEmpty(t, p.Verifier)
	assert.NotEmpty(t, p.Challenge)

	// 32 random bytes in base64url form, without padding, give 43 characters.
	assert.Len(t, p.Verifier, 43)
}

func TestGeneratePKCE_ChallengeMatchesVerifier(t *testing.T) {
	p, err := GeneratePKCE()
	require.NoError(t, err)

	// Hash the verifier with SHA-256. Then encode it as base64url, without
	// padding.
	h := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(h[:])

	assert.Equal(t, want, p.Challenge)
}

func TestGeneratePKCE_Unique(t *testing.T) {
	a, err := GeneratePKCE()
	require.NoError(t, err)

	b, err := GeneratePKCE()
	require.NoError(t, err)

	assert.NotEqual(t, a.Verifier, b.Verifier)
}
