package agent

import (
	"context"
	"errors"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Agent is the interface for an agentic conversation loop.
type Agent interface {
	// Run appends msgs to the conversation history and executes the
	// loop, returning the run's event stream. Zero msgs continues from
	// the current state. Cancel ctx to abort the run — the run ends
	// with ctx's error on the stream.
	//
	// Runs are sequential: calling Run while another run is active
	// fails the returned stream. Errors — including pre-flight ones —
	// surface on the stream, never as a panic or a lost run.
	Run(ctx context.Context, msgs ...ai.Message) *Stream

	// Messages returns a copy of the current conversation history.
	Messages() []ai.Message

	// Close releases backend resources (e.g. a CLI subprocess). For
	// purely in-process agents it is a no-op.
	Close() error
}

// Prompt sends a user message, blocks until the run completes, and
// returns the run's final assistant message. Convenience for
// Run + [Stream.Wait].
func Prompt(ctx context.Context, a Agent, input string) (*ai.Message, error) {
	msgs, err := a.Run(ctx, ai.UserMessage(input)).Wait()
	if err != nil {
		return nil, err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.RoleAssistant {
			return &msgs[i], nil
		}
	}
	return nil, errors.New("agent: run produced no assistant message")
}

// CreateFunc creates an [Agent] from a required model and options.
// Register one under a provider/kind name with [RegisterAgent]; [Create]
// looks it up by the spec's provider prefix.
type CreateFunc func(model ai.Model, opts ...Option) Agent

// config holds all configuration for the agent loop.
type config struct {
	model        ai.Model
	provider     ai.Provider
	tools        []ai.Tool
	history      []ai.Message
	systemPrompt string
	streamOpts   []ai.Option
	maxTurns     int
	hooks        hooks
	extensions   map[string]any
}

// Option configures an [Agent].
type Option func(*config)

// WithProvider sets the [ai.Provider] instance the agent uses for
// inference. When set, [Default] calls the provider directly and skips
// the global [ai.GetProvider] lookup keyed by [ai.Model.Provider]. This lets
// callers wire a provider per-agent without registering it in the
// process-wide registry.
func WithProvider(p ai.Provider) Option {
	return func(c *config) { c.provider = p }
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

// WithTools sets the tools available for the agent. Mix client-side
// function tools (e.g. [ai.DefineTool]) with provider-hosted server
// tools (e.g. [ai.DefineServerTool]) — the agent advertises both to
// the model and runs only the function tools locally.
func WithTools(tools ...ai.Tool) Option {
	return func(c *config) { c.tools = tools }
}

// WithHistory sets the initial conversation messages.
func WithHistory(msgs ...ai.Message) Option {
	return func(c *config) { c.history = msgs }
}

// WithSystemPrompt sets the system prompt.
func WithSystemPrompt(s string) Option {
	return func(c *config) { c.systemPrompt = s }
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
