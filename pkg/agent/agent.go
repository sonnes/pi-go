package agent

import (
	"context"
	"errors"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Agent is the interface for an agentic conversation loop.
type Agent interface {
	// Run appends msgs to the conversation history and runs the loop.
	// It returns the event stream of the run. With zero msgs, the loop
	// continues from the current state. If you cancel ctx, the run
	// stops and the stream ends with the context error.
	//
	// Runs are sequential. If another run is active, Run fails the
	// returned stream. All errors reach the stream, including
	// pre-flight errors. Run never panics and never loses a run.
	Run(ctx context.Context, msgs ...ai.Message) *Stream

	// Messages returns a copy of the current conversation history.
	Messages() []ai.Message

	// Close releases backend resources, for example a CLI subprocess.
	// For an in-process agent, Close does nothing.
	Close() error
}

// Prompt sends a user message and blocks until the run is complete. It
// returns the final assistant message of the run. Prompt is a shortcut
// for Run and [Stream.Wait].
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

// config holds all configuration for the agent loop.
type config struct {
	lm           ai.LanguageModel
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

// WithExtension stores a configuration value for a sub-package under key.
// A factory reads its own slot from [Config.Extensions].
func WithExtension(key string, value any) Option {
	return WithExtensionMutator(key, func(any) any { return value })
}

// WithExtensionMutator reads the current extension value under key,
// passes it to mutate, and stores the result. If the key is absent, the
// value is nil. A sub-package uses this option to compose several options
// onto one struct and to keep its internals private.
func WithExtensionMutator(key string, mutate func(any) any) Option {
	return func(c *config) {
		if c.extensions == nil {
			c.extensions = make(map[string]any)
		}
		c.extensions[key] = mutate(c.extensions[key])
	}
}

// WithTools sets the tools that the agent can use. You can mix
// client-side function tools, for example [ai.DefineTool], with
// provider-hosted server tools, for example [ai.DefineServerTool]. The
// agent advertises both kinds to the model. It runs only the function
// tools locally.
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

// WithStreamOpts sets the options that the agent passes to each LLM
// stream call.
func WithStreamOpts(opts ...ai.Option) Option {
	return func(c *config) { c.streamOpts = opts }
}

// WithMaxTurns limits the number of turns to prevent an infinite loop.
// Zero means no limit.
func WithMaxTurns(n int) Option {
	return func(c *config) { c.maxTurns = n }
}

// WithHook registers a lifecycle hook for the given event. If one event
// has several hooks, they run in registration order.
func WithHook(event HookEvent, h Hook) Option {
	return func(c *config) {
		if c.hooks == nil {
			c.hooks = make(hooks)
		}
		c.hooks[event] = append(c.hooks[event], h)
	}
}
