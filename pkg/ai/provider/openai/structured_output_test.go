package openai_test

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
	aiopenai "github.com/sonnes/pi-go/pkg/ai/provider/openai"
)

func TestGenerateObject_UsesStrictJSONSchema(t *testing.T) {
	client := &http.Client{Transport: openAIRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))

		format, ok := request["response_format"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "json_schema", format["type"])

		jsonSchema, ok := format["json_schema"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "structured_output", jsonSchema["name"])
		assert.Equal(t, true, jsonSchema["strict"])
		assert.NotEmpty(t, jsonSchema["schema"])

		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"chatcmpl_test",
				"object":"chat.completion",
				"created":0,
				"model":"gpt-test",
				"choices":[{"index":0,"message":{"role":"assistant","content":"{\"name\":\"Ada\"}"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
			}`)),
			Request: r,
		}, nil
	})}
	p := aiopenai.New(
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

type openAIRoundTripFunc func(*http.Request) (*http.Response, error)

func (f openAIRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
