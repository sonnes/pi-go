package ai

import "strings"

// Prompt holds the inputs for a model call: system prompt, messages, and tools.
type Prompt struct {
	System   string
	Messages []Message
	Tools    []ToolInfo
}

// Text returns the concatenated text of all messages, separated by newlines.
// It is used by modalities that take a single text prompt, such as image
// generation.
func (p Prompt) Text() string {
	parts := make([]string, 0, len(p.Messages))
	for _, m := range p.Messages {
		if t := m.Text(); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}
