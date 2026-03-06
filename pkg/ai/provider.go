package ai

import (
	"context"
	"sync"
)

// Provider is the core interface for message-based AI interactions.
// Use StreamText for streaming, or StreamText(...).Result() for synchronous completion.
type Provider interface {
	API() string
	StreamText(ctx context.Context, model Model, p Prompt, opts StreamOptions) *EventStream
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Provider)
)

// RegisterProvider registers a provider for the given API identifier.
func RegisterProvider(api string, p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[api] = p
}

// GetProvider returns the provider registered for the given API identifier.
func GetProvider(api string) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[api]
	return p, ok
}

// Providers returns all registered providers.
func Providers() []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	providers := make([]Provider, 0, len(registry))
	for _, p := range registry {
		providers = append(providers, p)
	}
	return providers
}

// UnregisterProvider removes the provider for the given API identifier.
func UnregisterProvider(api string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, api)
}

// ClearProviders removes all registered providers.
func ClearProviders() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Provider)
}
