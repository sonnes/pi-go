package ai

// ThinkingLevel controls reasoning depth, mapped to provider-specific params.
type ThinkingLevel string

const (
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

// StreamOptions holds configuration for stream and complete calls.
// Providers receive this directly; callers use Option functions.
type StreamOptions struct {
	Temperature   *float64
	MaxTokens     *int
	ThinkingLevel ThinkingLevel
	ToolChoice    ToolChoice
	Headers       map[string]string
	Metadata      map[string]any
}

// ToolChoice controls tool selection behavior.
type ToolChoice string

const (
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceRequired ToolChoice = "required"
)

// SpecificToolChoice creates a ToolChoice for a specific tool by name.
func SpecificToolChoice(name string) ToolChoice {
	return ToolChoice(name)
}

// Option configures a [StreamOptions] value.
type Option func(*StreamOptions)

// WithTemperature sets the sampling temperature.
func WithTemperature(t float64) Option {
	return func(o *StreamOptions) { o.Temperature = &t }
}

// WithMaxTokens sets the maximum number of output tokens.
func WithMaxTokens(n int) Option {
	return func(o *StreamOptions) { o.MaxTokens = &n }
}

// WithThinking sets the reasoning depth level.
func WithThinking(level ThinkingLevel) Option {
	return func(o *StreamOptions) { o.ThinkingLevel = level }
}

// WithToolChoice sets the tool selection behavior.
func WithToolChoice(choice ToolChoice) Option {
	return func(o *StreamOptions) { o.ToolChoice = choice }
}

// WithHeaders sets additional HTTP headers for the request.
func WithHeaders(h map[string]string) Option {
	return func(o *StreamOptions) { o.Headers = h }
}

// WithMetadata sets provider-specific metadata.
func WithMetadata(m map[string]any) Option {
	return func(o *StreamOptions) { o.Metadata = m }
}

// ApplyOptions builds a [StreamOptions] from the given option functions.
func ApplyOptions(opts []Option) StreamOptions {
	var o StreamOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
