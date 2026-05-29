package modelsdev_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/catalog/modelsdev"
)

func TestProviderFetchConvertsModelsDevRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api.json", r.URL.Path)
		w.Header().Set("ETag", `"fixture"`)
		_, _ = w.Write([]byte(`{
			"openai": {
				"name": "OpenAI",
				"api": "responses",
				"doc": "https://platform.openai.com/docs",
				"models": {
					"gpt-5.4": {
						"id": "gpt-5.4",
						"name": "GPT-5.4",
						"reasoning": true,
						"tool_call": true,
						"structured_output": true,
						"temperature": false,
						"modalities": {
							"input": ["text", "image"],
							"output": ["text"]
						},
						"limits": {
							"context": 400000,
							"output": 128000
						},
						"cost": {
							"input": 2,
							"output": 8,
							"cache_read": 0.2,
							"cache_write": 2.5
						},
						"knowledge": "2026-01-01",
						"release_date": "2026-02-03",
						"last_updated": "2026-05-28",
						"open_weights": false
					}
				}
			}
		}`))
	}))
	defer server.Close()

	provider := modelsdev.New(
		modelsdev.WithURL(server.URL+"/api.json"),
		modelsdev.WithHTTPClient(server.Client()),
	)

	cat, err := provider.Fetch(context.Background(), nil)
	require.NoError(t, err)

	require.Len(t, cat.Sources, 1)
	assert.Equal(t, "models.dev", cat.Sources[0].ID)
	assert.Equal(t, `"fixture"`, cat.Sources[0].ETag)

	require.Len(t, cat.Providers, 1)
	assert.Equal(t, "openai", cat.Providers[0].ID)
	assert.Equal(t, "responses", cat.Providers[0].API)

	require.Len(t, cat.Models, 1)
	model := cat.Models[0]
	assert.Equal(t, "gpt-5.4", model.ID)
	assert.Equal(t, "GPT-5.4", model.Name)
	assert.Equal(t, "responses", model.API)
	assert.Equal(t, "openai", model.Provider)
	assert.True(t, model.Reasoning)
	assert.True(t, model.ToolCall)
	assert.True(t, model.StructuredOutput)
	assert.False(t, model.Temperature)
	assert.Equal(t, []ai.Modality{ai.ModalityText, ai.ModalityImage}, model.Input)
	assert.Equal(t, 400_000, model.Limits.Context)
	assert.Equal(t, 128_000, model.Limits.Output)
	assert.InDelta(t, 2.0, model.Cost.Input, 0.0001)
	assert.Equal(t, "2026-01-01", model.Knowledge)
	assert.Equal(t, "2026-02-03", model.ReleaseDate)
	assert.Equal(t, "2026-05-28", model.LastUpdated)
	assert.Equal(t, []ai.ThinkingLevel{
		ai.ThinkingOff,
		ai.ThinkingMinimal,
		ai.ThinkingLow,
		ai.ThinkingMedium,
		ai.ThinkingHigh,
		ai.ThinkingXHigh,
	}, model.ThinkingLevels)
}
