package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAuthFrom_NoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	creds, err := LoadAuthFrom(path)
	require.NoError(t, err)
	assert.Empty(t, creds)
}

func TestSaveAndLoadAuth_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subdir", "auth.json")

	want := StoredCredentials{
		"anthropic": {
			AccessToken:  "access-1",
			RefreshToken: "refresh-1",
			ExpiresAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			ClientID:     "client-1",
		},
		"google": {
			AccessToken:  "access-2",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			ClientID:     "client-2",
			ClientSecret: "secret-2",
			Extras:       map[string]any{"projectId": "my-project"},
		},
	}

	err := SaveAuthTo(path, want)
	require.NoError(t, err)

	got, err := LoadAuthFrom(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSaveAuthTo_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")

	err := SaveAuthTo(path, StoredCredentials{})
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSaveAuthTo_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	path := filepath.Join(dir, "auth.json")

	err := SaveAuthTo(path, StoredCredentials{})
	require.NoError(t, err)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestStoredCredential_ToOAuthCredentials(t *testing.T) {
	sc := StoredCredential{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Extras:       map[string]any{"key": "value"},
	}

	creds := sc.ToOAuthCredentials()
	assert.Equal(t, "access", creds.AccessToken)
	assert.Equal(t, "refresh", creds.RefreshToken)
	assert.Equal(t, sc.ExpiresAt, creds.ExpiresAt)
	assert.Equal(t, "value", creds.Extras["key"])
}
