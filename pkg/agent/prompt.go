package agent

import "context"

// PromptSection is a named, lazily-rendered block in the system prompt.
type PromptSection interface {
	Key() string
	Content(ctx context.Context) string
}

// Prompt is a system prompt for an agent.
type Prompt struct {
	Sections []PromptSection
}
