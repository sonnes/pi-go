// Package catalog is the public registry for the pi SDK. It holds typed
// provider capabilities, their models, and custom agent factories, and resolves
// "<provider>/<model>" specs — or bare model IDs when unambiguous — to
// callable [ai.LanguageModel]s and [agent.Agent]s.
//
// A [Catalog] keeps an independent provider lookup for each [ai] capability
// while sharing one model index across them. Registration supplies identity
// and model metadata explicitly; provider implementations carry only behavior.
package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Catalog is a registry of providers, models, and agent factories. The
// zero value is unusable; create one with [New]. Safe for concurrent use.
type Catalog struct {
	mu              sync.RWMutex
	textProviders   map[string]ai.TextProvider
	imageProviders  map[string]ai.ImageProvider
	speechProviders map[string]ai.SpeechProvider
	models          map[string]ai.Model      // by "<provider>/<id>" and "<provider>/<alias>"
	agents          map[string]agent.Factory // by agent kind
}

// New returns an empty, ready-to-use catalog.
func New() *Catalog {
	return &Catalog{
		textProviders:   make(map[string]ai.TextProvider),
		imageProviders:  make(map[string]ai.ImageProvider),
		speechProviders: make(map[string]ai.SpeechProvider),
		models:          make(map[string]ai.Model),
		agents:          make(map[string]agent.Factory),
	}
}

// RegisterTextProvider registers p for text generation under id and adds
// models to the shared model index. Last write wins.
func (c *Catalog) RegisterTextProvider(
	id string,
	p ai.TextProvider,
	models ...ai.Model,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.textProviders[id] = p
	c.registerModelsLocked(id, models)
}

// RegisterImageProvider registers p for image generation under id and adds
// models to the shared model index. Last write wins.
func (c *Catalog) RegisterImageProvider(
	id string,
	p ai.ImageProvider,
	models ...ai.Model,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.imageProviders[id] = p
	c.registerModelsLocked(id, models)
}

// RegisterSpeechProvider registers p for speech generation under id and adds
// models to the shared model index. Last write wins.
func (c *Catalog) RegisterSpeechProvider(
	id string,
	p ai.SpeechProvider,
	models ...ai.Model,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.speechProviders[id] = p
	c.registerModelsLocked(id, models)
}

func (c *Catalog) registerModelsLocked(providerID string, models []ai.Model) {
	for _, m := range models {
		c.registerModelLocked(providerID, m)
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

// resolve looks up model metadata and its provider id for a
// spec. A full "<provider>/<model>" spec is looked up directly; a bare
// model ID (or alias) resolves when exactly one registered provider
// serves it, and errors listing the full specs when several do. The caller
// uses the id to select its capability-specific provider map.
func (c *Catalog) resolve(spec string) (ai.Model, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if m, ok := c.models[spec]; ok {
		providerID, _, _ := strings.Cut(spec, "/")
		return m, providerID, nil
	}
	return c.resolveBareLocked(spec)
}

// resolveBareLocked resolves a spec with no provider prefix by scanning
// every registered key for a matching model ID or alias.
func (c *Catalog) resolveBareLocked(id string) (ai.Model, string, error) {
	var (
		matches []string
		found   ai.Model
		foundID string
	)
	seen := map[string]bool{}
	for key, m := range c.models {
		providerID, modelID, _ := strings.Cut(key, "/")
		if modelID != id || seen[providerID] {
			continue
		}
		seen[providerID] = true
		matches = append(matches, key)
		found, foundID = m, providerID
	}
	switch len(matches) {
	case 0:
		return ai.Model{}, "", fmt.Errorf("catalog: unknown model %q", id)
	case 1:
		return found, foundID, nil
	default:
		sort.Strings(matches)
		return ai.Model{}, "", fmt.Errorf(
			"catalog: ambiguous model %q: use a full spec (%s)",
			id,
			strings.Join(matches, ", "),
		)
	}
}

// LanguageModel resolves a spec to a bound [ai.LanguageModel]. It errors if
// the spec is unknown or the provider does not support text generation.
func (c *Catalog) LanguageModel(spec string) (ai.LanguageModel, error) {
	m, providerID, err := c.resolve(spec)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	tp, ok := c.textProviders[providerID]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("catalog: provider %q does not support text generation", providerID)
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

// GenerateText resolves spec and blocks for a text response. Convenience
// wrapper around StreamText(...).Wait().
func (c *Catalog) GenerateText(
	ctx context.Context,
	spec string,
	p ai.Prompt,
	opts ...ai.Option,
) (*ai.Message, error) {
	return c.StreamText(ctx, spec, p, opts...).Wait()
}

// ImageModel resolves a spec to a bound [ai.ImageModel]. It errors if the
// spec is unknown or the provider does not support image generation.
func (c *Catalog) ImageModel(spec string) (ai.ImageModel, error) {
	m, providerID, err := c.resolve(spec)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	ip, ok := c.imageProviders[providerID]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("catalog: provider %q does not support image generation", providerID)
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

// SpeechModel resolves a spec to a bound [ai.SpeechModel]. It errors if the
// spec is unknown or the provider does not support speech generation.
func (c *Catalog) SpeechModel(spec string) (ai.SpeechModel, error) {
	m, providerID, err := c.resolve(spec)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	sp, ok := c.speechProviders[providerID]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("catalog: provider %q does not support speech generation", providerID)
	}
	return ai.NewSpeechModel(m, sp), nil
}

// GenerateSpeech resolves spec and generates audio from the prompt.
func (c *Catalog) GenerateSpeech(
	ctx context.Context,
	spec string,
	p ai.Prompt,
	opts ...ai.Option,
) (*ai.SpeechResponse, error) {
	sm, err := c.SpeechModel(spec)
	if err != nil {
		return nil, err
	}
	return sm.GenerateSpeech(ctx, p, opts...)
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
