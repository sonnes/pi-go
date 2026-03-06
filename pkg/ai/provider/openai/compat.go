package openai

import ai "github.com/sonnes/pi-go/pkg/ai"

// ThinkingFormat controls how reasoning/thinking parameters are sent.
type ThinkingFormat string

const (
	// ThinkingFormatOpenAI uses reasoning_effort (OpenAI default).
	ThinkingFormatOpenAI ThinkingFormat = "openai"
	// ThinkingFormatZAI uses thinking: { type: "enabled" | "disabled" }.
	ThinkingFormatZAI ThinkingFormat = "zai"
	// ThinkingFormatQwen uses enable_thinking: boolean.
	ThinkingFormatQwen ThinkingFormat = "qwen"
)

// Compat defines compatibility options for OpenAI-compatible APIs.
// Set on Model.Compat to control how request parameters are formatted.
// When nil or wrong type, DefaultCompat is used.
type Compat struct {
	// MaxTokensField selects the field name for max output tokens.
	// "max_tokens" for standard models, "max_completion_tokens" for reasoning models.
	MaxTokensField string

	// SupportsTemperature indicates whether the model accepts the temperature parameter.
	// Reasoning models (o3, o4) do not.
	SupportsTemperature bool

	// SupportsReasoningEffort indicates whether the model accepts reasoning_effort.
	// Only reasoning models (o3, o4) do.
	SupportsReasoningEffort bool

	// SupportsStore indicates whether the provider supports the store field.
	// When true, store: false is sent to disable storage.
	SupportsStore bool

	// SupportsDeveloperRole indicates whether the provider supports the developer role
	// (vs system) for system prompts. Used with reasoning models.
	SupportsDeveloperRole bool

	// SupportsUsageInStreaming indicates whether the provider supports
	// stream_options: { include_usage: true } for token usage in streaming responses.
	SupportsUsageInStreaming bool

	// SupportsStrictMode indicates whether the provider supports the strict field
	// in tool definitions. Some providers reject unknown fields.
	SupportsStrictMode bool

	// ThinkingFormat controls how reasoning/thinking parameters are sent.
	// Empty string defaults to "openai" behavior.
	ThinkingFormat ThinkingFormat

	// RequiresToolResultName indicates whether tool results require the name field.
	RequiresToolResultName bool

	// RequiresAssistantAfterToolResult indicates whether a synthetic assistant message
	// must be inserted between tool results and the next user message.
	RequiresAssistantAfterToolResult bool

	// RequiresThinkingAsText indicates whether thinking blocks must be converted to
	// text content instead of using a provider-specific thinking field.
	RequiresThinkingAsText bool

	// RequiresMistralToolIds indicates whether tool call IDs must be normalized to
	// Mistral format (exactly 9 alphanumeric characters).
	RequiresMistralToolIds bool
}

// CompatAPI implements ai.ProviderCompat.
func (Compat) CompatAPI() string { return "openai-completions" }

// getCompat extracts Compat from a model, falling back to DefaultCompat.
func getCompat(model ai.Model) Compat {
	if c, ok := model.Compat.(Compat); ok {
		return c
	}
	return DefaultCompat()
}

// DefaultCompat returns compat for standard OpenAI models (GPT-4, GPT-5, etc.).
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

// ReasoningCompat returns compat for OpenAI reasoning models (o3, o4, etc.).
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
