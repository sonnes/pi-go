package ai

import "sync"

// Registry holds providers (by id) and models (by "<provider>/<id>" spec).
// The zero value is unusable; create one with [NewRegistry]. Safe for
// concurrent use.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	models    map[string]Model
}

// NewRegistry returns an empty, ready-to-use registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		models:    make(map[string]Model),
	}
}

// RegisterProvider registers a provider under the given id (last write wins).
func (r *Registry) RegisterProvider(id string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[id] = p
}

// GetProvider returns the provider registered under id.
func (r *Registry) GetProvider(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

// Providers returns all registered providers.
func (r *Registry) Providers() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out
}

// UnregisterProvider removes the provider registered under id.
func (r *Registry) UnregisterProvider(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, id)
}

// ClearProviders removes all registered providers.
func (r *Registry) ClearProviders() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = make(map[string]Provider)
}

// RegisterModel registers m under its "<Provider>/<ID>" spec and each
// "<Provider>/<alias>" (last write wins).
func (r *Registry) RegisterModel(m Model) {
	r.mu.Lock()
	defer r.mu.Unlock()
	canonical := m.Provider + "/" + m.ID
	// Drop alias keys from a prior registration of the same model so a
	// removed alias does not keep resolving to the stale value.
	if prev, ok := r.models[canonical]; ok {
		for _, alias := range prev.Aliases {
			delete(r.models, prev.Provider+"/"+alias)
		}
	}
	r.models[canonical] = m
	for _, alias := range m.Aliases {
		r.models[m.Provider+"/"+alias] = m
	}
}

// ResolveModel returns the model registered under the given "<provider>/<id>"
// (or "<provider>/<alias>") spec.
func (r *Registry) ResolveModel(spec string) (Model, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[spec]
	return m, ok
}

// Models returns all registered models, deduplicated across aliases.
func (r *Registry) Models() []Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Model, 0, len(r.models))
	for spec, m := range r.models {
		if spec == m.Provider+"/"+m.ID {
			out = append(out, m)
		}
	}
	return out
}

// UnregisterModel removes the model identified by spec and all its alias specs.
func (r *Registry) UnregisterModel(spec string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.models[spec]
	if !ok {
		return
	}
	delete(r.models, m.Provider+"/"+m.ID)
	for _, alias := range m.Aliases {
		delete(r.models, m.Provider+"/"+alias)
	}
}

// ClearModels removes all registered models.
func (r *Registry) ClearModels() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = make(map[string]Model)
}

// defaultRegistry backs the package-level functions.
var defaultRegistry = NewRegistry()

// RegisterProvider registers a provider for the given id in the default registry.
func RegisterProvider(id string, p Provider) { defaultRegistry.RegisterProvider(id, p) }

// GetProvider returns the provider registered for the given id.
func GetProvider(id string) (Provider, bool) { return defaultRegistry.GetProvider(id) }

// Providers returns all providers in the default registry.
func Providers() []Provider { return defaultRegistry.Providers() }

// UnregisterProvider removes the provider for the given id.
func UnregisterProvider(id string) { defaultRegistry.UnregisterProvider(id) }

// ClearProviders removes all providers from the default registry.
func ClearProviders() { defaultRegistry.ClearProviders() }

// RegisterModel registers a model in the default registry.
func RegisterModel(m Model) { defaultRegistry.RegisterModel(m) }

// ResolveModel resolves a "<provider>/<id>" spec in the default registry.
func ResolveModel(spec string) (Model, bool) { return defaultRegistry.ResolveModel(spec) }

// Models returns all models in the default registry.
func Models() []Model { return defaultRegistry.Models() }

// UnregisterModel removes the model for the given spec.
func UnregisterModel(spec string) { defaultRegistry.UnregisterModel(spec) }

// ClearModels removes all models from the default registry.
func ClearModels() { defaultRegistry.ClearModels() }
