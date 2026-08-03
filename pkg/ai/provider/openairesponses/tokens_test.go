package openairesponses_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
)

func TestCountTokens(t *testing.T) {
	client := &http.Client{Transport: responsesRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/v1/responses/input_tokens", r.URL.Path)

		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, "gpt-test", request["model"])
		assert.Equal(t, "Be concise.", request["instructions"])
		assert.Contains(t, request, "input")

		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body:    io.NopCloser(strings.NewReader(`{"object":"response.input_tokens","input_tokens":42}`)),
			Request: r,
		}, nil
	})}
	p := openairesponses.New(
		option.WithAPIKey("test-key"),
		option.WithHTTPClient(client),
	)

	count, err := p.CountTokens(
		context.Background(),
		ai.Model{ID: "gpt-test"},
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
