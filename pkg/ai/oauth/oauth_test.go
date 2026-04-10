package oauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCredentials_IsExpired(t *testing.T) {
	tests := []struct {
		name string
		exp  time.Time
		want bool
	}{
		{
			name: "expired in the past",
			exp:  time.Now().Add(-1 * time.Hour),
			want: true,
		},
		{
			name: "expires within safety margin",
			exp:  time.Now().Add(3 * time.Minute),
			want: true,
		},
		{
			name: "valid with time remaining",
			exp:  time.Now().Add(1 * time.Hour),
			want: false,
		},
		{
			name: "zero time is expired",
			exp:  time.Time{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Credentials{ExpiresAt: tt.exp}
			assert.Equal(t, tt.want, c.IsExpired())
		})
	}
}

func TestTokenRefresherFunc(t *testing.T) {
	called := false
	want := Credentials{AccessToken: "new-token"}

	fn := TokenRefresherFunc(func(ctx context.Context, creds Credentials) (Credentials, error) {
		called = true
		return want, nil
	})

	got, err := fn.RefreshToken(t.Context(), Credentials{})
	assert.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, want.AccessToken, got.AccessToken)
}
