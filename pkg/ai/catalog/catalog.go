package catalog

import (
	"context"
	"sort"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Source describes an upstream source used to build a [Catalog].
type Source struct {
	ID   string `json:"id"`
	URL  string `json:"url,omitempty"`
	ETag string `json:"etag,omitempty"`
}

// ProviderInfo describes a provider represented in a [Catalog].
type ProviderInfo struct {
	ID               string            `json:"id"`
	SourceProvider   string            `json:"source_provider,omitempty"`
	Vendor           string            `json:"vendor,omitempty"`
	Auth             string            `json:"auth,omitempty"`
	Label            string            `json:"label,omitempty"`
	API              string            `json:"api,omitempty"`
	Env              []string          `json:"env,omitempty"`
	Models           []string          `json:"models,omitempty"`
	RecommendedTiers map[string]string `json:"recommended_tiers,omitempty"`
}

// Catalog contains model and provider rows from one or more catalog providers.
type Catalog struct {
	SchemaVersion int            `json:"schema_version"`
	Source        string         `json:"source,omitempty"`
	Sources       []Source       `json:"sources,omitempty"`
	RefreshedAt   time.Time      `json:"refreshed_at,omitempty"`
	ExpiresAt     time.Time      `json:"expires_at,omitempty"`
	Providers     []ProviderInfo `json:"providers,omitempty"`
	Models        []ai.Model     `json:"models,omitempty"`
}

// CatalogProvider fetches catalog rows from one source.
type CatalogProvider interface {
	ID() string
	Fetch(ctx context.Context, previous *Catalog) (*Catalog, error)
}

// Merge combines catalogs. Later catalogs win when provider/model keys collide.
func Merge(catalogs ...Catalog) Catalog {
	merged := Catalog{
		SchemaVersion: 1,
	}
	sourcesByKey := make(map[string]Source)
	providersByID := make(map[string]ProviderInfo)
	modelsByKey := make(map[string]ai.Model)

	for _, cat := range catalogs {
		if cat.SchemaVersion > merged.SchemaVersion {
			merged.SchemaVersion = cat.SchemaVersion
		}
		if cat.Source != "" {
			merged.Source = cat.Source
		}
		if cat.RefreshedAt.After(merged.RefreshedAt) {
			merged.RefreshedAt = cat.RefreshedAt
		}
		if cat.ExpiresAt.After(merged.ExpiresAt) {
			merged.ExpiresAt = cat.ExpiresAt
		}
		for _, source := range cat.Sources {
			sourcesByKey[sourceKey(source)] = source
		}
		for _, provider := range cat.Providers {
			providersByID[provider.ID] = provider
		}
		for _, model := range cat.Models {
			modelsByKey[modelKey(model.Provider, model.ID)] = model
		}
	}

	merged.Sources = make([]Source, 0, len(sourcesByKey))
	for _, source := range sourcesByKey {
		merged.Sources = append(merged.Sources, source)
	}
	sort.Slice(merged.Sources, func(i, j int) bool {
		return sourceKey(merged.Sources[i]) < sourceKey(merged.Sources[j])
	})

	merged.Providers = make([]ProviderInfo, 0, len(providersByID))
	for _, provider := range providersByID {
		merged.Providers = append(merged.Providers, provider)
	}
	sort.Slice(merged.Providers, func(i, j int) bool {
		return merged.Providers[i].ID < merged.Providers[j].ID
	})

	merged.Models = make([]ai.Model, 0, len(modelsByKey))
	for _, model := range modelsByKey {
		merged.Models = append(merged.Models, model)
	}
	sort.Slice(merged.Models, func(i, j int) bool {
		left := modelKey(merged.Models[i].Provider, merged.Models[i].ID)
		right := modelKey(merged.Models[j].Provider, merged.Models[j].ID)
		return left < right
	})

	return merged
}

func sourceKey(source Source) string {
	return source.ID + "\x00" + source.URL
}

func modelKey(provider string, model string) string {
	return provider + "\x00" + model
}
