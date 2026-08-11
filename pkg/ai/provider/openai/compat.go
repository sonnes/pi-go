package openai

import ai "github.com/sonnes/pi-go/pkg/ai"

// ThinkingFormat selects how the provider sends the reasoning parameters.
type ThinkingFormat string

const (
	// ThinkingFormatOpenAI uses reasoning_effort. It is the OpenAI default.
	ThinkingFormatOpenAI ThinkingFormat = "openai"
	// ThinkingFormatZAI uses thinking: { type: "enabled" | "disabled" }.
	ThinkingFormatZAI ThinkingFormat = "zai"
	// ThinkingFormatQwen uses enable_thinking: boolean.
	ThinkingFormatQwen ThinkingFormat = "qwen"
)

// Compat holds the compatibility options for OpenAI-compatible APIs.
// Set it on Model.Compat to control the format of the request parameters.
// If Model.Compat is nil or has a different type, the provider uses
// DefaultCompat.
type Compat struct {
	// MaxTokensField selects the field name for the maximum output tokens.
	// Standard models use "max_tokens". Reasoning models use
	// "max_completion_tokens".
	MaxTokensField string

	// SupportsTemperature reports whether the model accepts the temperature
	// parameter. Reasoning models (o3, o4) do not accept it.
	SupportsTemperature bool

	// SupportsReasoningEffort reports whether the model accepts
	// reasoning_effort. Only reasoning models (o3, o4) accept it.
	SupportsReasoningEffort bool

	// SupportsStore reports whether the provider accepts the store field.
	// If it is true, the request sends store: false to turn storage off.
	SupportsStore bool

	// SupportsDeveloperRole reports whether the provider accepts the
	// developer role for system prompts, in place of the system role.
	// Reasoning models use it.
	SupportsDeveloperRole bool

	// SupportsUsageInStreaming reports whether the provider accepts
	// stream_options: { include_usage: true }. That option adds the token
	// usage to a streaming response.
	SupportsUsageInStreaming bool

	// SupportsStrictMode reports whether the provider accepts the strict
	// field in a tool definition. Some providers reject unknown fields.
	SupportsStrictMode bool

	// ThinkingFormat selects how the provider sends the reasoning
	// parameters. The empty string selects the "openai" behavior.
	ThinkingFormat ThinkingFormat

	// RequiresToolResultName reports whether a tool result needs the name
	// field.
	RequiresToolResultName bool

	// RequiresAssistantAfterToolResult reports whether the provider needs a
	// synthetic assistant message between the tool results and the next
	// user message.
	RequiresAssistantAfterToolResult bool

	// RequiresThinkingAsText reports whether the provider needs thinking
	// blocks as text content, in place of a provider-specific thinking
	// field.
	RequiresThinkingAsText bool

	// RequiresMistralToolIds reports whether the provider needs tool call
	// IDs in the Mistral format (exactly 9 alphanumeric characters).
	RequiresMistralToolIds bool
}

// CompatAPI implements ai.ProviderCompat.
func (Compat) CompatAPI() string { return "openai-completions" }

// getCompat extracts Compat from a model. If the model carries none, it
// returns DefaultCompat.
func getCompat(model ai.Model) Compat {
	if c, ok := model.Compat.(Compat); ok {
		return c
	}
	return DefaultCompat()
}

// DefaultCompat returns the compat for standard OpenAI models such as GPT-4
// and GPT-5.
func DefaultCompat() Compat {
	return Compat{
		MaxTokensField:           "max_completion_tokens",
		SupportsTemperature:      true,
		SupportsReasoningEffort:  false,
		SupportsStore:            true,
		SupportsDeveloperRole:    true,
		SupportsUsageInStreaming: true,
		SupportsStrictMode:       true,
		ThinkingFormat:           ThinkingFormatOpenAI,
	}
}

// ReasoningCompat returns the compat for OpenAI reasoning models such as o3
// and o4.
func ReasoningCompat() Compat {
	return Compat{
		MaxTokensField:           "max_completion_tokens",
		SupportsTemperature:      false,
		SupportsReasoningEffort:  true,
		SupportsStore:            true,
		SupportsDeveloperRole:    true,
		SupportsUsageInStreaming: true,
		SupportsStrictMode:       true,
		ThinkingFormat:           ThinkingFormatOpenAI,
	}
}
