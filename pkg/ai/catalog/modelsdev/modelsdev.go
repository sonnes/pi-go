package modelsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/catalog"
)

const defaultURL = "https://models.dev/api.json"

// Provider fetches catalog rows from models.dev.
type Provider struct {
	url        string
	httpClient *http.Client
}

// Option configures a [Provider].
type Option func(*Provider)

// WithURL sets the models.dev API URL.
func WithURL(url string) Option {
	return func(p *Provider) {
		p.url = url
	}
}

// WithHTTPClient sets the HTTP client used by the provider.
func WithHTTPClient(client *http.Client) Option {
	return func(p *Provider) {
		p.httpClient = client
	}
}

// New creates a models.dev catalog provider.
func New(opts ...Option) *Provider {
	p := &Provider{
		url:        defaultURL,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ID returns the catalog provider identifier.
func (p *Provider) ID() string {
	return "models.dev"
}

// Fetch retrieves and converts models.dev rows into a [catalog.Catalog].
func (p *Provider) Fetch(ctx context.Context, previous *catalog.Catalog) (*catalog.Catalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return nil, fmt.Errorf("modelsdev: create request: %w", err)
	}
	if etag := previousETag(previous); etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("modelsdev: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && previous != nil {
		return previous, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("modelsdev: fetch status %d", resp.StatusCode)
	}

	var upstream map[string]providerRow
	if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
		return nil, fmt.Errorf("modelsdev: decode: %w", err)
	}

	cat := catalog.Catalog{
		SchemaVersion: 1,
		Source:        p.ID(),
		Sources: []catalog.Source{
			{
				ID:   p.ID(),
				URL:  p.url,
				ETag: resp.Header.Get("ETag"),
			},
		},
		RefreshedAt: time.Now().UTC(),
	}

	providerIDs := make([]string, 0, len(upstream))
	for id := range upstream {
		providerIDs = append(providerIDs, id)
	}
	sort.Strings(providerIDs)

	for _, providerID := range providerIDs {
		row := upstream[providerID]
		info := catalog.ProviderInfo{
			ID:     providerID,
			Vendor: providerID,
			Label:  firstNonEmpty(row.Name, providerID),
			API:    row.API,
		}

		modelIDs := make([]string, 0, len(row.Models))
		for modelID := range row.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)
		info.Models = modelIDs
		cat.Providers = append(cat.Providers, info)

		for _, modelID := range modelIDs {
			model := convertModel(providerID, row.API, modelID, row.Models[modelID])
			cat.Models = append(cat.Models, model)
		}
	}

	return &cat, nil
}

type providerRow struct {
	Name   string              `json:"name"`
	API    string              `json:"api"`
	Models map[string]modelRow `json:"models"`
}

type modelRow struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	API              string        `json:"api"`
	Reasoning        bool          `json:"reasoning"`
	ToolCall         bool          `json:"tool_call"`
	StructuredOutput bool          `json:"structured_output"`
	Temperature      bool          `json:"temperature"`
	Modalities       modalitiesRow `json:"modalities"`
	Limits           limitsRow     `json:"limits"`
	Cost             costRow       `json:"cost"`
	Knowledge        string        `json:"knowledge"`
	ReleaseDate      string        `json:"release_date"`
	LastUpdated      string        `json:"last_updated"`
	OpenWeights      bool          `json:"open_weights"`
	Status           string        `json:"status"`
}

type modalitiesRow struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type limitsRow struct {
	Context int `json:"context"`
	Input   int `json:"input"`
	Output  int `json:"output"`
}

type costRow struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	CacheRead   float64 `json:"cache_read"`
	CacheWrite  float64 `json:"cache_write"`
	Reasoning   float64 `json:"reasoning"`
	InputAudio  float64 `json:"input_audio"`
	OutputAudio float64 `json:"output_audio"`
}

func convertModel(providerID string, providerAPI string, modelID string, row modelRow) ai.Model {
	id := firstNonEmpty(row.ID, modelID)
	api := firstNonEmpty(row.API, providerAPI)
	return ai.Model{
		ID:               id,
		Name:             firstNonEmpty(row.Name, id),
		API:              api,
		Provider:         providerID,
		Reasoning:        row.Reasoning,
		ThinkingLevels:   thinkingLevels(row.Reasoning),
		ToolCall:         row.ToolCall,
		StructuredOutput: row.StructuredOutput,
		Temperature:      row.Temperature,
		Input:            convertModalities(row.Modalities.Input),
		Output:           convertModalities(row.Modalities.Output),
		Limits: ai.Limits{
			Context: row.Limits.Context,
			Input:   row.Limits.Input,
			Output:  row.Limits.Output,
		},
		Cost: ai.Cost{
			Input:       row.Cost.Input,
			Output:      row.Cost.Output,
			CacheRead:   row.Cost.CacheRead,
			CacheWrite:  row.Cost.CacheWrite,
			Reasoning:   row.Cost.Reasoning,
			InputAudio:  row.Cost.InputAudio,
			OutputAudio: row.Cost.OutputAudio,
		},
		Knowledge:   row.Knowledge,
		ReleaseDate: row.ReleaseDate,
		LastUpdated: row.LastUpdated,
		OpenWeights: row.OpenWeights,
		Status:      row.Status,
	}
}

func convertModalities(values []string) []ai.Modality {
	out := make([]ai.Modality, 0, len(values))
	for _, value := range values {
		switch ai.Modality(value) {
		case ai.ModalityText,
			ai.ModalityImage,
			ai.ModalityPDF,
			ai.ModalityAudio,
			ai.ModalityVideo:
			out = append(out, ai.Modality(value))
		}
	}
	return out
}

func thinkingLevels(reasoning bool) []ai.ThinkingLevel {
	if !reasoning {
		return []ai.ThinkingLevel{ai.ThinkingOff}
	}
	return []ai.ThinkingLevel{
		ai.ThinkingOff,
		ai.ThinkingMinimal,
		ai.ThinkingLow,
		ai.ThinkingMedium,
		ai.ThinkingHigh,
		ai.ThinkingXHigh,
	}
}

func previousETag(previous *catalog.Catalog) string {
	if previous == nil {
		return ""
	}
	for _, source := range previous.Sources {
		if source.ID == "models.dev" {
			return source.ETag
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
