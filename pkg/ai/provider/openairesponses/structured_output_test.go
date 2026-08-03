package openairesponses_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
)

func TestGenerateObject_UsesStrictJSONSchema(t *testing.T) {
	client := &http.Client{Transport: responsesRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))

		textConfig, ok := request["text"].(map[string]any)
		require.True(t, ok)
		format, ok := textConfig["format"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "json_schema", format["type"])
		assert.Equal(t, "structured_output", format["name"])
		assert.Equal(t, true, format["strict"])
		assert.NotEmpty(t, format["schema"])

		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"resp_test",
				"object":"response",
				"model":"gpt-test",
				"status":"completed",
				"output":[{"id":"msg_test","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{\"name\":\"Ada\"}","annotations":[]}]}],
				"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}
			}`)),
			Request: r,
		}, nil
	})}
	p := openairesponses.New(
		option.WithAPIKey("test-key"),
		option.WithHTTPClient(client),
	)

	schema, err := jsonschema.For[struct {
		Name string `json:"name"`
	}](nil)
	require.NoError(t, err)

	response, err := p.GenerateObject(
		context.Background(),
		ai.Model{ID: "gpt-test"},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("Ada")}},
		schema,
		ai.StreamOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, `{"name":"Ada"}`, response.Raw)
}

type responsesRoundTripFunc func(*http.Request) (*http.Response, error)

func (f responsesRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
