package agent

import (
	"sync"

	"github.com/sonnes/pi-go/pkg/ai"
)

var (
	factoriesMu sync.RWMutex
	factories   = make(map[string]Factory)
)

// RegisterFactory registers an agent [Factory] under the given name.
// Re-registering the same name replaces the previous entry.
func RegisterFactory(name string, f Factory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories[name] = f
}

// GetFactory returns the factory registered under name.
func GetFactory(name string) (Factory, bool) {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	f, ok := factories[name]
	return f, ok
}

// Factories returns a copy of the registry. Callers may mutate the
// returned map freely without affecting the registry.
func Factories() map[string]Factory {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	out := make(map[string]Factory, len(factories))
	for name, f := range factories {
		out[name] = f
	}
	return out
}

// UnregisterFactory removes the factory registered under name.
func UnregisterFactory(name string) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	delete(factories, name)
}

// ClearFactories removes all registered factories. Intended for test
// isolation; most callers should not use this.
func ClearFactories() {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories = make(map[string]Factory)
}

// Config is a snapshot of the configuration applied by a set of [Option]
// values. It is the exported view of the internal config struct, for use
// by factory implementations that need to read options without importing
// unexported types.
//
// Extensions holds values set by [WithExtension] and [WithExtensionMutator],
// keyed by the owning sub-package. Factories look up their own key to
// pull agent-specific configuration out of the unified option stream.
//
// Convention: sub-packages should use their own package name as the
// extension key, matching the name they register under via [RegisterFactory].
// For example, pkg/agent/claude uses the key "claude" for both. This keeps
// key collisions avoidable by construction and makes factories easy to find.
type Config struct {
	Model        ai.Model
	Provider     ai.Provider
	Tools        []ai.Tool
	History      []Message
	SystemPrompt string
	StreamOpts   []ai.Option
	MaxTurns     int
	Extensions   map[string]any
}

// ApplyOptions applies opts and returns the resulting [Config] snapshot.
// Factory implementations registered in the agent registry use this to
// translate agent-level [Option] values into their own configuration.
func ApplyOptions(opts ...Option) Config {
	c := config{}
	for _, opt := range opts {
		opt(&c)
	}
	return Config{
		Model:        c.model,
		Provider:     c.provider,
		Tools:        c.tools,
		History:      c.history,
		SystemPrompt: c.systemPrompt,
		StreamOpts:   c.streamOpts,
		MaxTurns:     c.maxTurns,
		Extensions:   c.extensions,
	}
}
