// Package catalog is the public registry for the pi SDK. It holds
// providers, their models, and custom agent factories, and resolves
// "<provider>/<model>" specs to callable [ai.LanguageModel]s and
// [agent.Agent]s.
//
// A [Catalog] is the single home for provider identity and routing —
// the [ai] capability interfaces (TextProvider, ImageProvider, …) carry
// only behavior. Register a provider once and every model it lists
// becomes available as both a raw language model and an agent.
package catalog

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Provider is what [Catalog.RegisterProvider] accepts: identity plus the
// list of models it serves. A registered provider must also implement at
// least one capability interface ([ai.TextProvider], [ai.ImageProvider],
// …) for its models to resolve to something callable.
type Provider interface {
	// Provider returns the provider identity, e.g. "anthropic-messages".
	Provider() string
	// Models returns the models this provider serves.
	Models() []ai.Model
}

// Catalog is a registry of providers, models, and agent factories. The
// zero value is unusable; create one with [New]. Safe for concurrent use.
type Catalog struct {
	mu        sync.RWMutex
	providers map[string]Provider
	models    map[string]ai.Model      // by "<provider>/<id>" and "<provider>/<alias>"
	agents    map[string]agent.Factory // by agent kind
}

// New returns an empty, ready-to-use catalog.
func New() *Catalog {
	return &Catalog{
		providers: make(map[string]Provider),
		models:    make(map[string]ai.Model),
		agents:    make(map[string]agent.Factory),
	}
}

// RegisterProvider registers p under its identity and ingests every model
// it serves (keyed under "<provider>/<id>" and each alias). Last write wins.
func (c *Catalog) RegisterProvider(p Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := p.Provider()
	c.providers[id] = p
	for _, m := range p.Models() {
		c.registerModelLocked(id, m)
	}
}

// RegisterModel registers a single model under the given provider id.
func (c *Catalog) RegisterModel(providerID string, m ai.Model) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registerModelLocked(providerID, m)
}

func (c *Catalog) registerModelLocked(providerID string, m ai.Model) {
	c.models[providerID+"/"+m.ID] = m
	for _, alias := range m.Aliases {
		c.models[providerID+"/"+alias] = m
	}
}

// RegisterAgent registers a custom agent factory under an agent kind (the
// spec's prefix, e.g. "claude-cli"). Last write wins.
func (c *Catalog) RegisterAgent(kind string, f agent.Factory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agents[kind] = f
}

// resolve looks up the model metadata and its registered provider for a
// "<provider>/<model>" spec. Capability (text/image/object) is asserted by
// the caller against the returned provider.
func (c *Catalog) resolve(spec string) (ai.Model, Provider, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	m, ok := c.models[spec]
	if !ok {
		return ai.Model{}, nil, fmt.Errorf("catalog: unknown model %q", spec)
	}
	providerID, _, _ := strings.Cut(spec, "/")
	p, ok := c.providers[providerID]
	if !ok {
		return ai.Model{}, nil, fmt.Errorf("catalog: no provider registered for %q", providerID)
	}
	return m, p, nil
}

// LanguageModel resolves a spec to a bound [ai.LanguageModel]. It errors if
// the spec is unknown or the provider does not support text generation.
func (c *Catalog) LanguageModel(spec string) (ai.LanguageModel, error) {
	m, p, err := c.resolve(spec)
	if err != nil {
		return nil, err
	}
	tp, ok := p.(ai.TextProvider)
	if !ok {
		return nil, fmt.Errorf("catalog: provider %q does not support text generation", p.Provider())
	}
	return ai.NewLanguageModel(m, tp), nil
}

// StreamText resolves spec and streams a text response. A resolution
// error surfaces on the returned stream (via [ai.ErrStream]) rather than a
// separate error return; block for the final message with Wait().
func (c *Catalog) StreamText(
	ctx context.Context,
	spec string,
	p ai.Prompt,
	opts ...ai.Option,
) *ai.EventStream {
	lm, err := c.LanguageModel(spec)
	if err != nil {
		return ai.ErrStream(err)
	}
	return lm.StreamText(ctx, p, opts...)
}

// ImageModel resolves a spec to a bound [ai.ImageModel]. It errors if the
// spec is unknown or the provider does not support image generation.
func (c *Catalog) ImageModel(spec string) (ai.ImageModel, error) {
	m, p, err := c.resolve(spec)
	if err != nil {
		return nil, err
	}
	ip, ok := p.(ai.ImageProvider)
	if !ok {
		return nil, fmt.Errorf("catalog: provider %q does not support image generation", p.Provider())
	}
	return ai.NewImageModel(m, ip), nil
}

// GenerateImage resolves spec and generates images from the prompt.
func (c *Catalog) GenerateImage(
	ctx context.Context,
	spec string,
	p ai.Prompt,
	opts ...ai.Option,
) (*ai.ImageResponse, error) {
	im, err := c.ImageModel(spec)
	if err != nil {
		return nil, err
	}
	return im.GenerateImage(ctx, p, opts...)
}

// GenerateObject resolves spec to a language model and generates a typed
// object. The model's provider must also implement [ai.ObjectProvider].
// It is a function, not a method, because Go methods cannot be generic.
func GenerateObject[T any](
	ctx context.Context,
	c *Catalog,
	spec string,
	p ai.Prompt,
	opts ...ai.Option,
) (*ai.ObjectResult[T], error) {
	lm, err := c.LanguageModel(spec)
	if err != nil {
		return nil, err
	}
	return ai.GenerateObject[T](ctx, lm, p, opts...)
}

// Agent builds an agent for a "<kind>/<model>" spec. A registered custom
// factory for the kind wins; otherwise the spec resolves to a language
// model wrapped in the default [agent.New] loop.
func (c *Catalog) Agent(spec string, opts ...agent.Option) (agent.Agent, error) {
	kind, _, _ := strings.Cut(spec, "/")

	c.mu.RLock()
	f, custom := c.agents[kind]
	c.mu.RUnlock()
	if custom {
		return f(spec, opts...)
	}

	lm, err := c.LanguageModel(spec)
	if err != nil {
		return nil, err
	}
	return agent.New(lm, opts...), nil
}
