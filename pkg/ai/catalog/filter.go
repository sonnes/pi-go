package catalog

import "github.com/sonnes/pi-go/pkg/ai"

// ProviderPolicy describes how a provider should be included in a filtered
// catalog.
type ProviderPolicy struct{}

// Filter controls which providers are retained in a catalog.
type Filter struct {
	Providers map[string]ProviderPolicy
}

// ApplyFilter returns a catalog containing only providers allowed by filter.
func ApplyFilter(cat Catalog, filter Filter) Catalog {
	if len(filter.Providers) == 0 {
		return cat
	}

	allowed := make(map[string]struct{}, len(filter.Providers))
	for id := range filter.Providers {
		allowed[id] = struct{}{}
	}

	filtered := cat
	filtered.Providers = nil
	for _, provider := range cat.Providers {
		if _, ok := allowed[provider.ID]; !ok {
			continue
		}
		filtered.Providers = append(filtered.Providers, provider)
	}

	filtered.Models = nil
	for _, model := range cat.Models {
		if _, ok := allowed[model.Provider]; !ok {
			continue
		}
		filtered.Models = append(filtered.Models, model)
	}

	filtered.Models = cloneModels(filtered.Models)
	return filtered
}

func cloneModels(models []ai.Model) []ai.Model {
	out := make([]ai.Model, len(models))
	copy(out, models)
	return out
}
