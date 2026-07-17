package ai

import "context"

// Provider is the core interface for message-based AI interactions.
// Use StreamText for streaming, or StreamText(...).Wait() for synchronous completion.
type Provider interface {
	// Provider returns the provider identifier (e.g. "anthropic-messages").
	Provider() string
	StreamText(ctx context.Context, model Model, p Prompt, opts StreamOptions) *EventStream
}
