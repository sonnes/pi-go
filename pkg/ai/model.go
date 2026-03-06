package ai

// Modality represents an input modality a model supports.
type Modality string

const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
)

// Model describes an AI model and its capabilities.
type Model struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Aliases       []string          `json:"aliases,omitempty"`
	API           string            `json:"api"`
	Provider      string            `json:"provider"`
	BaseURL       string            `json:"base_url,omitempty"`
	Reasoning     bool              `json:"reasoning,omitempty"`
	Input         []Modality        `json:"input,omitempty"`
	Output        []Modality        `json:"output,omitempty"`
	Cost          Cost              `json:"cost,omitzero"`
	ContextWindow int               `json:"context_window,omitempty"`
	MaxTokens     int               `json:"max_tokens,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Compat        ProviderCompat    `json:"-"`
}

// ProviderCompat is implemented by provider-specific compat structs.
type ProviderCompat interface {
	CompatAPI() string
}

// Cost defines per-million-token pricing in USD.
type Cost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// CalculateCost computes the cost breakdown for a model response.
func CalculateCost(model Model, usage Usage) UsageCost {
	c := UsageCost{
		Input:      float64(usage.Input) * model.Cost.Input / 1_000_000,
		Output:     float64(usage.Output) * model.Cost.Output / 1_000_000,
		CacheRead:  float64(usage.CacheRead) * model.Cost.CacheRead / 1_000_000,
		CacheWrite: float64(usage.CacheWrite) * model.Cost.CacheWrite / 1_000_000,
	}
	c.Total = c.Input + c.Output + c.CacheRead + c.CacheWrite
	return c
}
