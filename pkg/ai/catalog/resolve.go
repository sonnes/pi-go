package catalog

import (
	"fmt"

	"github.com/sonnes/pi-go/pkg/ai"
)

// ModelRef identifies a model row by provider and model ID.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Selection is the resolved model and thinking level for one catalog candidate.
type Selection struct {
	Provider           string           `json:"provider"`
	ModelID            string           `json:"model"`
	Model              ai.Model         `json:"-"`
	RequestedLevel     ai.ThinkingLevel `json:"requested_level"`
	EffectiveLevel     ai.ThinkingLevel `json:"effective_level"`
	CandidateIndex     int              `json:"candidate_index"`
	CandidateCount     int              `json:"candidate_count"`
	CatalogVersion     int              `json:"catalog_version"`
	CatalogRefreshedAt string           `json:"catalog_refreshed_at"`
	LevelDegraded      bool             `json:"level_degraded,omitempty"`
}

// Resolve resolves one model reference and normalizes its thinking level.
func Resolve(cat Catalog, ref ModelRef, level ai.ThinkingLevel) (Selection, error) {
	model, ok := findModel(cat, ref)
	if !ok {
		return Selection{}, fmt.Errorf(
			"catalog: model %s/%s not found",
			ref.Provider,
			ref.Model,
		)
	}

	effectiveLevel, degraded := ai.ResolveThinkingLevel(model, level)
	return Selection{
		Provider:           model.Provider,
		ModelID:            model.ID,
		Model:              model,
		RequestedLevel:     level,
		EffectiveLevel:     effectiveLevel,
		CandidateIndex:     0,
		CandidateCount:     1,
		CatalogVersion:     cat.SchemaVersion,
		CatalogRefreshedAt: cat.RefreshedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		LevelDegraded:      degraded,
	}, nil
}

func findModel(cat Catalog, ref ModelRef) (ai.Model, bool) {
	for _, model := range cat.Models {
		if model.Provider == ref.Provider && model.ID == ref.Model {
			return model, true
		}
	}
	return ai.Model{}, false
}
