package ai

// Prompt holds the inputs for a model call: system prompt, messages, and tools.
type Prompt struct {
	System   string
	Messages []Message
	Tools    []ToolInfo
}
