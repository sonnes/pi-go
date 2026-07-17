package ai

// Modality represents an input modality a model supports.
type Modality string

const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityPDF   Modality = "pdf"
	ModalityAudio Modality = "audio"
	ModalityVideo Modality = "video"
)

// Model describes an AI model and its capabilities. It is pure intrinsic
// metadata — it carries no provider identity and no credentials, so the
// same value can be bound to any provider that serves it (see
// [NewLanguageModel]).
type Model struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Aliases          []string          `json:"aliases,omitempty"`
	BaseURL          string            `json:"base_url,omitempty"`
	Reasoning        bool              `json:"reasoning,omitempty"`
	ThinkingLevels   []ThinkingLevel   `json:"thinking_levels,omitempty"`
	ToolCall         bool              `json:"tool_call,omitempty"`
	StructuredOutput bool              `json:"structured_output,omitempty"`
	Temperature      bool              `json:"temperature,omitempty"`
	Input            []Modality        `json:"input,omitempty"`
	Output           []Modality        `json:"output,omitempty"`
	Cost             Cost              `json:"cost,omitzero"`
	ContextWindow    int               `json:"context_window,omitempty"`
	MaxTokens        int               `json:"max_tokens,omitempty"`
	Knowledge        string            `json:"knowledge,omitempty"`
	ReleaseDate      string            `json:"release_date,omitempty"`
	LastUpdated      string            `json:"last_updated,omitempty"`
	OpenWeights      bool              `json:"open_weights,omitempty"`
	Status           string            `json:"status,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Compat           ProviderCompat    `json:"-"`
}

// ProviderCompat is implemented by provider-specific compat structs.
type ProviderCompat interface {
	CompatAPI() string
}

// Cost defines per-million-token pricing in USD.
type Cost struct {
	Input       float64 `json:"input,omitempty"`
	Output      float64 `json:"output,omitempty"`
	CacheRead   float64 `json:"cache_read,omitempty"`
	CacheWrite  float64 `json:"cache_write,omitempty"`
	Reasoning   float64 `json:"reasoning,omitempty"`
	InputAudio  float64 `json:"input_audio,omitempty"`
	OutputAudio float64 `json:"output_audio,omitempty"`
}

// CalculateCost computes the cost breakdown for a model response.
func CalculateCost(model Model, usage Usage) UsageCost {
	c := UsageCost{
		Input:       float64(usage.Input) * model.Cost.Input / 1_000_000,
		Output:      float64(usage.Output) * model.Cost.Output / 1_000_000,
		CacheRead:   float64(usage.CacheRead) * model.Cost.CacheRead / 1_000_000,
		CacheWrite:  float64(usage.CacheWrite) * model.Cost.CacheWrite / 1_000_000,
		Reasoning:   float64(usage.Reasoning) * model.Cost.Reasoning / 1_000_000,
		InputAudio:  float64(usage.InputAudio) * model.Cost.InputAudio / 1_000_000,
		OutputAudio: float64(usage.OutputAudio) * model.Cost.OutputAudio / 1_000_000,
	}
	c.Total = c.Input +
		c.Output +
		c.CacheRead +
		c.CacheWrite +
		c.Reasoning +
		c.InputAudio +
		c.OutputAudio
	return c
}

// Usage contains token usage statistics for a model response.
type Usage struct {
	Input       int
	Output      int
	CacheRead   int
	CacheWrite  int
	Reasoning   int
	InputAudio  int
	OutputAudio int
	Total       int
	Cost        UsageCost
}

// UsageCost contains cost breakdown in USD.
type UsageCost struct {
	Input       float64
	Output      float64
	CacheRead   float64
	CacheWrite  float64
	Reasoning   float64
	InputAudio  float64
	OutputAudio float64
	Total       float64
}
