package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Registry holds agent constructors (by provider/kind name) and agent-managed
// models (by "<kind>/<id>" spec). The zero value is unusable; create one with
// [NewRegistry]. Safe for concurrent use.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]CreateFunc
	models *ai.Registry
}

// NewRegistry returns an empty, ready-to-use registry.
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]CreateFunc),
		models: ai.NewRegistry(),
	}
}

// RegisterAgent registers fn under name — the provider/kind prefix
// [Registry.Create] routes on (last write wins). The package-level
// [RegisterAgent] is generic and usually more convenient; this method takes the
// type-erased [CreateFunc].
func (r *Registry) RegisterAgent(name string, fn CreateFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[name] = fn
}

// GetAgent returns the create func registered under name.
func (r *Registry) GetAgent(name string) (CreateFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.agents[name]
	return fn, ok
}

// Agents returns a copy of the registered create funcs.
func (r *Registry) Agents() map[string]CreateFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]CreateFunc, len(r.agents))
	for name, fn := range r.agents {
		out[name] = fn
	}
	return out
}

// UnregisterAgent removes the create func registered under name.
func (r *Registry) UnregisterAgent(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, name)
}

// ClearAgents removes all registered create funcs.
func (r *Registry) ClearAgents() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = make(map[string]CreateFunc)
}

// RegisterModel registers an agent-managed model (e.g. spec "claude/sonnet").
func (r *Registry) RegisterModel(m ai.Model) { r.models.RegisterModel(m) }

// ResolveModel resolves a "<kind>/<id>" spec to an agent-managed model.
func (r *Registry) ResolveModel(spec string) (ai.Model, bool) { return r.models.ResolveModel(spec) }

// Models returns all registered agent-managed models.
func (r *Registry) Models() []ai.Model { return r.models.Models() }

// UnregisterModel removes the agent-managed model for the given spec.
func (r *Registry) UnregisterModel(spec string) { r.models.UnregisterModel(spec) }

// ClearModels removes all registered agent-managed models.
func (r *Registry) ClearModels() { r.models.ClearModels() }

// Create builds an agent for the "<provider>/<model>" spec. The provider prefix
// selects a registered create func (the CLI kinds); any other prefix falls back
// to the [New] Default agent, resolving the model via the ai registry. Returns
// an error when the spec is unknown.
func (r *Registry) Create(model string, opts ...Option) (Agent, error) {
	provider, id, _ := strings.Cut(model, "/")
	if fn, ok := r.GetAgent(provider); ok {
		m, ok := r.ResolveModel(model)
		if !ok {
			m = ai.Model{Provider: provider, ID: id} // CLI just needs the name
		}
		return fn(m, opts...), nil
	}
	m, ok := ai.ResolveModel(model)
	if !ok {
		return nil, fmt.Errorf("agent: unknown model %q", model)
	}
	return New(m, opts...), nil
}

// defaultRegistry backs the package-level functions.
var defaultRegistry = NewRegistry()

// RegisterAgent registers an agent constructor under name in the default
// registry. It is generic over the concrete return type, so a package's New func
// registers directly with no adapter: agent.RegisterAgent("claude", claude.New).
func RegisterAgent[T Agent](name string, fn func(model ai.Model, opts ...Option) T) {
	defaultRegistry.RegisterAgent(name, func(m ai.Model, opts ...Option) Agent { return fn(m, opts...) })
}

// GetAgent returns the create func registered under name in the default registry.
func GetAgent(name string) (CreateFunc, bool) { return defaultRegistry.GetAgent(name) }

// Agents returns a copy of the default registry's create funcs.
func Agents() map[string]CreateFunc { return defaultRegistry.Agents() }

// UnregisterAgent removes the create func registered under name.
func UnregisterAgent(name string) { defaultRegistry.UnregisterAgent(name) }

// ClearAgents removes all create funcs from the default registry.
func ClearAgents() { defaultRegistry.ClearAgents() }

// RegisterModel registers an agent-managed model in the default registry.
func RegisterModel(m ai.Model) { defaultRegistry.RegisterModel(m) }

// ResolveModel resolves a "<kind>/<id>" spec in the default registry.
func ResolveModel(spec string) (ai.Model, bool) { return defaultRegistry.ResolveModel(spec) }

// Models returns all agent-managed models in the default registry.
func Models() []ai.Model { return defaultRegistry.Models() }

// UnregisterModel removes the agent-managed model for the given spec.
func UnregisterModel(spec string) { defaultRegistry.UnregisterModel(spec) }

// ClearModels removes all agent-managed models from the default registry.
func ClearModels() { defaultRegistry.ClearModels() }

// Create builds an agent for the spec via the default registry.
func Create(model string, opts ...Option) (Agent, error) {
	return defaultRegistry.Create(model, opts...)
}

// Config is a snapshot of the configuration applied by a set of [Option]
// values — the exported view of the internal config, for factory
// implementations that read options without importing unexported types.
//
// Extensions holds values set by [WithExtension]/[WithExtensionMutator],
// keyed by the owning sub-package (by convention its package name, matching
// the name it registers under via [RegisterAgent]).
type Config struct {
	Provider     ai.Provider
	Tools        []ai.Tool
	History      []Message
	SystemPrompt string
	StreamOpts   []ai.Option
	MaxTurns     int
	Extensions   map[string]any
}

// ApplyOptions applies opts and returns the resulting [Config] snapshot.
// The model is passed separately to constructors, not via options.
func ApplyOptions(opts ...Option) Config {
	c := config{}
	for _, opt := range opts {
		opt(&c)
	}
	return Config{
		Provider:     c.provider,
		Tools:        c.tools,
		History:      c.history,
		SystemPrompt: c.systemPrompt,
		StreamOpts:   c.streamOpts,
		MaxTurns:     c.maxTurns,
		Extensions:   c.extensions,
	}
}
