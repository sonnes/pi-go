package agent

import (
	"context"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/prompt"
)

// Agent is the interface for an agentic conversation loop.
type Agent interface {
	Send(ctx context.Context, input string) *EventStream
	SendMessages(ctx context.Context, msgs ...Message) *EventStream
	Continue(ctx context.Context) *EventStream
	Messages() []Message
	IsRunning() bool
	Err() error
}

// Factory creates an [Agent] from a model and options.
type Factory func(model ai.Model, opts ...Option) Agent

// config holds all configuration for the agent loop.
type config struct {
	model        ai.Model
	tools        []ai.Tool
	history      []Message
	systemPrompt prompt.Prompt
	streamOpts   []ai.Option
	maxTurns     int
	hooks        hooks
}

// Option configures an [Agent].
type Option func(*config)

// WithTools sets the tools available for the agent to call.
func WithTools(tools ...ai.Tool) Option {
	return func(c *config) { c.tools = tools }
}

// WithHistory sets the initial conversation messages.
func WithHistory(msgs ...Message) Option {
	return func(c *config) { c.history = msgs }
}

// WithSystemPrompt sets the system prompt.
func WithSystemPrompt(p prompt.Prompt) Option {
	return func(c *config) { c.systemPrompt = p }
}

// WithStreamOpts sets options passed to each LLM stream call.
func WithStreamOpts(opts ...ai.Option) Option {
	return func(c *config) { c.streamOpts = opts }
}

// WithMaxTurns limits the number of turns to prevent infinite loops.
// Zero means unlimited.
func WithMaxTurns(n int) Option {
	return func(c *config) { c.maxTurns = n }
}

// WithHook registers a lifecycle hook for the given event. Multiple
// hooks per event run in registration order.
func WithHook(event HookEvent, h Hook) Option {
	return func(c *config) {
		if c.hooks == nil {
			c.hooks = make(hooks)
		}
		c.hooks[event] = append(c.hooks[event], h)
	}
}
