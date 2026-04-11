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

	// 32 random bytes base64url-encoded without padding = 43 characters.
	assert.Len(t, p.Verifier, 43)
}

func TestGeneratePKCE_ChallengeMatchesVerifier(t *testing.T) {
	p, err := GeneratePKCE()
	require.NoError(t, err)

	// SHA-256 hash the verifier, then base64url-encode without padding.
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
