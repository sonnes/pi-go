package ai

// ThinkingLevel controls reasoning depth. Each provider maps it to its own
// parameters.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
	ThinkingMax     ThinkingLevel = "max"
)

var thinkingRanks = map[ThinkingLevel]int{
	ThinkingOff:     0,
	ThinkingMinimal: 1,
	ThinkingLow:     2,
	ThinkingMedium:  3,
	ThinkingHigh:    4,
	ThinkingXHigh:   5,
	ThinkingMax:     6,
}

// NormalizeThinkingLevel maps the empty level to [ThinkingOff].
func NormalizeThinkingLevel(level ThinkingLevel) ThinkingLevel {
	if level == "" {
		return ThinkingOff
	}
	return level
}

// ResolveThinkingLevel returns the closest thinking level that model supports.
//
// If the model does not support a positive level, the result is the highest
// supported level that is less than the requested one. An unknown level
// resolves to [ThinkingOff]. A model without thinking support also resolves
// to [ThinkingOff].
func ResolveThinkingLevel(model Model, requested ThinkingLevel) (ThinkingLevel, bool) {
	normalized := NormalizeThinkingLevel(requested)
	requestedRank, ok := thinkingRanks[normalized]
	if !ok {
		return ThinkingOff, true
	}

	if normalized == ThinkingOff {
		return ThinkingOff, false
	}

	best := ThinkingOff
	bestRank := 0
	for _, level := range model.ThinkingLevels {
		level = NormalizeThinkingLevel(level)
		rank, ok := thinkingRanks[level]
		if !ok {
			continue
		}
		if level == normalized {
			return normalized, false
		}
		if rank <= requestedRank && rank > bestRank {
			best = level
			bestRank = rank
		}
	}

	return best, best != normalized
}

// EffectiveThinkingLevel returns the level that a request uses. It is the one
// place that decides the level, so every provider gets the same answer.
//
// An explicit [ThinkingOff] stays off. An empty level takes
// [Model.DefaultThinkingLevel], which is empty for a model that declares no
// levels. A model that declares no levels does not clamp the request, because
// the catalog knows nothing about it. Every other level goes through
// [ResolveThinkingLevel].
func EffectiveThinkingLevel(model Model, requested ThinkingLevel) ThinkingLevel {
	if requested == "" {
		return NormalizeThinkingLevel(model.DefaultThinkingLevel)
	}

	if requested == ThinkingOff {
		return ThinkingOff
	}

	if len(model.ThinkingLevels) == 0 {
		return requested
	}

	level, _ := ResolveThinkingLevel(model, requested)
	return level
}

// CacheRetention controls prompt-cache breakpoint placement and TTL across
// providers. [CacheRetentionDefault] is equal to [CacheRetentionShort].
// Providers emit cache markers automatically, so a caller gets cache hits
// without extra configuration.
type CacheRetention string

const (
	// CacheRetentionDefault is the zero value and resolves to Short.
	CacheRetentionDefault CacheRetention = ""
	// CacheRetentionNone turns off all cache markers.
	CacheRetentionNone CacheRetention = "none"
	// CacheRetentionShort requests the default ephemeral TTL of the
	// provider (Anthropic: 5 minutes).
	CacheRetentionShort CacheRetention = "short"
	// CacheRetentionLong requests the longer ephemeral TTL, where the
	// provider supports it (Anthropic: 1 hour on api.anthropic.com).
	CacheRetentionLong CacheRetention = "long"
)

// ResolveCacheRetention returns the effective retention. It substitutes
// [CacheRetentionShort] for the zero value. Provider adapters call it, so the
// default-on behavior lives in exactly one place.
func ResolveCacheRetention(r CacheRetention) CacheRetention {
	if r == CacheRetentionDefault {
		return CacheRetentionShort
	}
	return r
}

// StreamOptions holds the configuration for stream and complete calls.
// Providers receive this value directly. Callers use Option functions.
type StreamOptions struct {
	Temperature    *float64
	MaxTokens      *int
	ThinkingLevel  ThinkingLevel
	ToolChoice     ToolChoice
	CacheRetention CacheRetention
	SessionID      string
	Headers        map[string]string
	Metadata       map[string]any
	ImageSize      string
	ImageCount     int
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

// WithCacheRetention sets the prompt-cache retention level. See
// [CacheRetention] for the available values. If the value is unset, the
// behavior is the same as [CacheRetentionShort].
func WithCacheRetention(r CacheRetention) Option {
	return func(o *StreamOptions) { o.CacheRetention = r }
}

// WithSessionID sets a stable session identifier for cache affinity. OpenAI
// Chat Completions and Responses support it and forward it as
// prompt_cache_key. Other providers ignore it.
func WithSessionID(id string) Option {
	return func(o *StreamOptions) { o.SessionID = id }
}

// WithHeaders sets additional HTTP headers for the request.
func WithHeaders(h map[string]string) Option {
	return func(o *StreamOptions) { o.Headers = h }
}

// WithMetadata sets provider-specific metadata.
func WithMetadata(m map[string]any) Option {
	return func(o *StreamOptions) { o.Metadata = m }
}

// WithImageSize sets the image dimensions for image generation, for example
// "1024x1024". A provider that does not support the value ignores it.
func WithImageSize(size string) Option {
	return func(o *StreamOptions) { o.ImageSize = size }
}

// WithImageCount sets the number of images to generate. If the value is 0 or
// less, the provider default applies (one image).
func WithImageCount(n int) Option {
	return func(o *StreamOptions) { o.ImageCount = n }
}

// ApplyOptions builds a [StreamOptions] from the given option functions.
func ApplyOptions(opts []Option) StreamOptions {
	var o StreamOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
