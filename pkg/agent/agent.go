package agent

import (
	"context"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/prompt"
	"github.com/sonnes/pi-go/pkg/pubsub"
)

// Agent is the interface for an agentic conversation loop.
//
// Agent embeds [pubsub.Subscriber] so consumers can subscribe to
// the agent's event stream. Events from all [Agent.Send] calls flow
// through a single broker per agent. Use [pubsub.After] to replay
// buffered events.
type Agent interface {
	pubsub.Subscriber[Event]
	Send(ctx context.Context, input string) error
	SendMessages(ctx context.Context, msgs ...Message) error
	Continue(ctx context.Context) error
	Wait(ctx context.Context) ([]ai.Message, error)
	Close()
	Messages() []Message
	IsRunning() bool
	Err() error
}

// Factory creates an [Agent] from options. Model and any sub-package
// configuration flow through the [Option] stream; see [WithModel],
// [WithModelName], and [WithExtensionMutator].
type Factory func(opts ...Option) Agent

// config holds all configuration for the agent loop.
type config struct {
	model        ai.Model
	provider     ai.Provider
	tools        []ai.Tool
	history      []Message
	systemPrompt prompt.Prompt
	streamOpts   []ai.Option
	maxTurns     int
	hooks        hooks
	extensions   map[string]any
}

// Option configures an [Agent].
type Option func(*config)

// WithModel sets the full [ai.Model]. Used by [Default], which needs
// [ai.Model.API] to route to a provider.
func WithModel(m ai.Model) Option {
	return func(c *config) { c.model = m }
}

// WithProvider sets the [ai.Provider] instance the agent uses for
// inference. When set, [Default] calls the provider directly and skips
// the global [ai.GetProvider] lookup keyed by [ai.Model.API]. This lets
// callers wire a provider per-agent without registering it in the
// process-wide registry.
func WithProvider(p ai.Provider) Option {
	return func(c *config) { c.provider = p }
}

// WithModelName sets the model identifier as a string. Used by agents
// that manage their own model catalog (e.g. the Claude CLI), which only
// need a name to pass through. Sets both [ai.Model.ID] and [ai.Model.Name];
// other fields on any previously-set [ai.Model] are preserved.
func WithModelName(name string) Option {
	return func(c *config) {
		c.model.ID = name
		c.model.Name = name
	}
}

// WithExtension stores a sub-package configuration value under key.
// Factories read [Config.Extensions] to pull out their own slot.
func WithExtension(key string, value any) Option {
	return WithExtensionMutator(key, func(any) any { return value })
}

// WithExtensionMutator reads the current extension value under key (or
// nil if absent), passes it to mutate, and stores the result. This lets
// sub-packages compose multiple options that layer onto a single struct
// without exposing their internals.
func WithExtensionMutator(key string, mutate func(any) any) Option {
	return func(c *config) {
		if c.extensions == nil {
			c.extensions = make(map[string]any)
		}
		c.extensions[key] = mutate(c.extensions[key])
	}
}

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
