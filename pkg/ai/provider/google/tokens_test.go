package google_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/google"
)

func TestCountTokens(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, ":countTokens"))

		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Contains(t, request, "contents")

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"totalTokens":42}`)),
			Request:    r,
		}, nil
	})}

	p, err := google.New(
		google.WithAPIKey("test-key"),
		google.WithHTTPClient(client),
	)
	require.NoError(t, err)

	count, err := p.CountTokens(
		context.Background(),
		ai.Model{ID: "gemini-test"},
		ai.Prompt{
			Messages: []ai.Message{
				ai.UserMessage("Hello"),
			},
		},
		ai.StreamOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, 42, count.Total)
}

// The Developer API's countTokens endpoint takes contents only — the SDK
// itself rejects systemInstruction and tools as Vertex-only parameters.
// Counting them away silently would understate the request, so both are
// refused up front with an error that names the real constraint.
func TestCountTokens_RejectsPromptFeaturesTheAPICannotCount(t *testing.T) {
	p, err := google.New(google.WithAPIKey("test-key"))
	require.NoError(t, err)

	tests := []struct {
		name    string
		prompt  ai.Prompt
		wantErr string
	}{
		{
			name: "system instruction",
			prompt: ai.Prompt{
				System:   "Be concise.",
				Messages: []ai.Message{ai.UserMessage("Hello")},
			},
			wantErr: "cannot count system instructions",
		},
		{
			name: "tool definitions",
			prompt: ai.Prompt{
				Messages: []ai.Message{ai.UserMessage("Hello")},
				Tools: []ai.ToolInfo{
					{
						Name:        "get_weather",
						Description: "Look up the weather",
						InputSchema: &jsonschema.Schema{Type: "object"},
					},
				},
			},
			wantErr: "cannot count tool definitions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.CountTokens(
				context.Background(),
				ai.Model{ID: "gemini-test"},
				tt.prompt,
				ai.StreamOptions{},
			)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
