package ai

// Modality represents an input modality that a model supports.
type Modality string

const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityPDF   Modality = "pdf"
	ModalityAudio Modality = "audio"
	ModalityVideo Modality = "video"
)

// Model describes an AI model and its capabilities. It is pure intrinsic
// metadata. It carries no provider identity and no credentials. As a result,
// you can bind the same value to any provider that serves it. See
// [NewLanguageModel].
type Model struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Aliases        []string        `json:"aliases,omitempty"`
	BaseURL        string          `json:"base_url,omitempty"`
	Reasoning      bool            `json:"reasoning,omitempty"`
	ThinkingLevels []ThinkingLevel `json:"thinking_levels,omitempty"`
	// DefaultThinkingLevel is the level that a request uses when the caller
	// asks for none. It is always a member of ThinkingLevels. An empty value
	// means that the model declares no levels.
	DefaultThinkingLevel ThinkingLevel     `json:"default_thinking_level,omitempty"`
	ToolCall             bool              `json:"tool_call,omitempty"`
	StructuredOutput     bool              `json:"structured_output,omitempty"`
	Temperature          bool              `json:"temperature,omitempty"`
	Input                []Modality        `json:"input,omitempty"`
	Output               []Modality        `json:"output,omitempty"`
	Cost                 Cost              `json:"cost,omitzero"`
	ContextWindow        int               `json:"context_window,omitempty"`
	MaxTokens            int               `json:"max_tokens,omitempty"`
	Knowledge            string            `json:"knowledge,omitempty"`
	ReleaseDate          string            `json:"release_date,omitempty"`
	LastUpdated          string            `json:"last_updated,omitempty"`
	OpenWeights          bool              `json:"open_weights,omitempty"`
	Status               string            `json:"status,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	Compat               ProviderCompat    `json:"-"`
}

// ProviderCompat is the interface that provider-specific compat structs
// implement.
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

// Usage contains token usage statistics for a model response.
//
// The categories are disjoint. Input counts uncached input tokens only. To
// get a grand total, a caller sums the fields it needs. There is no Total
// field, because providers do not agree on what belongs in one.
//
// The JSON tags are the wire format. [Message] stores usage with them. An
// application that stores a Usage of its own uses the same tags.
type Usage struct {
	Input       int       `json:"input,omitempty"`
	Output      int       `json:"output,omitempty"`
	CacheRead   int       `json:"cache_read,omitempty"`
	CacheWrite  int       `json:"cache_write,omitempty"`
	Reasoning   int       `json:"reasoning,omitempty"`
	InputAudio  int       `json:"input_audio,omitempty"`
	OutputAudio int       `json:"output_audio,omitempty"`
	Cost        UsageCost `json:"cost,omitzero"`
}

// Add returns the category-wise sum of two usage values. It is the
// accumulator for usage across turns and across runs.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		Input:       u.Input + other.Input,
		Output:      u.Output + other.Output,
		CacheRead:   u.CacheRead + other.CacheRead,
		CacheWrite:  u.CacheWrite + other.CacheWrite,
		Reasoning:   u.Reasoning + other.Reasoning,
		InputAudio:  u.InputAudio + other.InputAudio,
		OutputAudio: u.OutputAudio + other.OutputAudio,
		Cost:        u.Cost.Add(other.Cost),
	}
}

// UsageCost contains the cost breakdown in USD. It mirrors the categories of
// [Usage]. Like Usage, it carries no total. Sum the categories to get a total.
type UsageCost struct {
	Input       float64 `json:"input,omitempty"`
	Output      float64 `json:"output,omitempty"`
	CacheRead   float64 `json:"cache_read,omitempty"`
	CacheWrite  float64 `json:"cache_write,omitempty"`
	Reasoning   float64 `json:"reasoning,omitempty"`
	InputAudio  float64 `json:"input_audio,omitempty"`
	OutputAudio float64 `json:"output_audio,omitempty"`
}

// Add returns the category-wise sum of two cost breakdowns.
func (c UsageCost) Add(other UsageCost) UsageCost {
	return UsageCost{
		Input:       c.Input + other.Input,
		Output:      c.Output + other.Output,
		CacheRead:   c.CacheRead + other.CacheRead,
		CacheWrite:  c.CacheWrite + other.CacheWrite,
		Reasoning:   c.Reasoning + other.Reasoning,
		InputAudio:  c.InputAudio + other.InputAudio,
		OutputAudio: c.OutputAudio + other.OutputAudio,
	}
}
