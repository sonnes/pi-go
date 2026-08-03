package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	aianthropic "github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
)

func TestCountTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/messages/count_tokens", r.URL.Path)

		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, "claude-test", request["model"])
		assert.Equal(t, "Be concise.", request["system"])

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"input_tokens":42}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	p := aianthropic.New(
		aianthropic.WithAPIKey("test-key"),
		aianthropic.WithBaseURL(server.URL),
		aianthropic.WithHTTPClient(server.Client()),
	)

	count, err := p.CountTokens(
		context.Background(),
		ai.Model{ID: "claude-test"},
		ai.Prompt{
			System: "Be concise.",
			Messages: []ai.Message{
				ai.UserMessage("Hello"),
			},
		},
		ai.StreamOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, 42, count.Total)
}
