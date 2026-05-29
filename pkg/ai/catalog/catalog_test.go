package catalog_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/catalog"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	refreshed := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	expires := refreshed.Add(24 * time.Hour)
	want := catalog.Catalog{
		SchemaVersion: 1,
		Sources: []catalog.Source{
			{
				ID:   "models.dev",
				URL:  "https://models.dev/api.json",
				ETag: `"abc123"`,
			},
		},
		Providers: []catalog.ProviderInfo{
			{
				ID:             "anthropic:api",
				SourceProvider: "anthropic",
				Vendor:         "anthropic",
				Auth:           "api",
				Label:          "Anthropic API",
				Models:         []string{"claude-sonnet-4-6"},
			},
		},
		Models: []ai.Model{
			{
				ID:       "claude-sonnet-4-6",
				Name:     "Claude Sonnet 4.6",
				API:      "anthropic-messages",
				Provider: "anthropic:api",
				Limits: ai.Limits{
					Context: 200_000,
					Output:  64_000,
				},
				ThinkingLevels: []ai.ThinkingLevel{
					ai.ThinkingOff,
					ai.ThinkingLow,
					ai.ThinkingHigh,
				},
			},
		},
		RefreshedAt: refreshed,
		ExpiresAt:   expires,
	}

	err := catalog.SaveAtomic(path, want)
	require.NoError(t, err)

	got, err := catalog.Load(path)
	require.NoError(t, err)

	assert.Equal(t, want.SchemaVersion, got.SchemaVersion)
	assert.Equal(t, want.Sources, got.Sources)
	assert.Equal(t, want.Providers, got.Providers)
	assert.Equal(t, want.Models, got.Models)
	assert.Equal(t, want.RefreshedAt, got.RefreshedAt)
	assert.Equal(t, want.ExpiresAt, got.ExpiresAt)
}

func TestApplyFilterKeepsConfiguredProviders(t *testing.T) {
	cat := catalog.Catalog{
		Providers: []catalog.ProviderInfo{
			{
				ID:     "openai:api",
				Models: []string{"gpt-5.4"},
			},
			{
				ID:     "anthropic:api",
				Models: []string{"claude-sonnet-4-6"},
			},
		},
		Models: []ai.Model{
			{
				ID:       "gpt-5.4",
				Provider: "openai:api",
			},
			{
				ID:       "claude-sonnet-4-6",
				Provider: "anthropic:api",
			},
		},
	}

	got := catalog.ApplyFilter(
		cat,
		catalog.Filter{
			Providers: map[string]catalog.ProviderPolicy{
				"openai:api": {},
			},
		},
	)

	require.Len(t, got.Providers, 1)
	require.Len(t, got.Models, 1)
	assert.Equal(t, "openai:api", got.Providers[0].ID)
	assert.Equal(t, "gpt-5.4", got.Models[0].ID)
}

func TestResolveModelAndThinkingLevel(t *testing.T) {
	cat := catalog.Catalog{
		SchemaVersion: 1,
		Models: []ai.Model{
			{
				ID:       "gpt-5.4",
				API:      "responses",
				Provider: "openai:api",
				Limits: ai.Limits{
					Context: 400_000,
					Output:  128_000,
				},
				ThinkingLevels: []ai.ThinkingLevel{
					ai.ThinkingOff,
					ai.ThinkingLow,
					ai.ThinkingHigh,
				},
			},
		},
		RefreshedAt: time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
	}

	got, err := catalog.Resolve(
		cat,
		catalog.ModelRef{
			Provider: "openai:api",
			Model:    "gpt-5.4",
		},
		ai.ThinkingMedium,
	)
	require.NoError(t, err)

	assert.Equal(t, "openai:api", got.Provider)
	assert.Equal(t, "gpt-5.4", got.ModelID)
	assert.Equal(t, ai.ThinkingMedium, got.RequestedLevel)
	assert.Equal(t, ai.ThinkingLow, got.EffectiveLevel)
	assert.True(t, got.LevelDegraded)
	assert.Equal(t, 1, got.CandidateCount)
	assert.Equal(t, 0, got.CandidateIndex)
	assert.Equal(t, 400_000, got.Model.Limits.Context)
}

func TestMergeDeduplicatesAndSortsModels(t *testing.T) {
	left := catalog.Catalog{
		Sources: []catalog.Source{{ID: "left"}},
		Models: []ai.Model{
			{
				ID:       "z",
				Provider: "p2",
			},
			{
				ID:       "a",
				Provider: "p1",
				Name:     "old",
			},
		},
	}
	right := catalog.Catalog{
		Sources: []catalog.Source{{ID: "right"}},
		Models: []ai.Model{
			{
				ID:       "a",
				Provider: "p1",
				Name:     "new",
			},
		},
	}

	got := catalog.Merge(left, right)

	require.Len(t, got.Sources, 2)
	require.Len(t, got.Models, 2)
	assert.Equal(t, "p1", got.Models[0].Provider)
	assert.Equal(t, "a", got.Models[0].ID)
	assert.Equal(t, "new", got.Models[0].Name)
	assert.Equal(t, "p2", got.Models[1].Provider)
	assert.Equal(t, "z", got.Models[1].ID)
}
