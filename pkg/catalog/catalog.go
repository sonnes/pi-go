// Package catalog is the public registry for the pi SDK. It holds typed
// provider capabilities, their models, and custom agent factories. It resolves
// a "<provider>/<model>" spec to a callable [ai.LanguageModel] or
// [agent.Agent]. It also resolves a bare model ID when only one provider
// serves that model.
//
// A [Catalog] keeps an independent provider lookup for each [ai] capability,
// and all the lookups share one model index. Registration supplies the identity
// and the model metadata explicitly. A provider implementation carries only
// behavior.
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
// zero value does not work. [New] creates a usable catalog. A Catalog is
// safe for concurrent use.
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

// RegisterTextProvider registers p for text generation under id. It also adds
// the models to the shared model index. The last write wins.
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

// RegisterImageProvider registers p for image generation under id. It also
// adds the models to the shared model index. The last write wins.
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

// RegisterSpeechProvider registers p for speech generation under id. It also
// adds the models to the shared model index. The last write wins.
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

// RegisterAgent registers a custom agent factory under an agent kind. The
// kind is the prefix of the spec, for example "claude-cli". The last write
// wins.
func (c *Catalog) RegisterAgent(kind string, f agent.Factory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agents[kind] = f
}

// resolve looks up the model metadata and the provider id for a spec.
// It looks up a full "<provider>/<model>" spec directly. A bare model ID
// or alias resolves when exactly one registered provider serves it. If
// several providers serve it, resolve returns an error that lists the full
// specs. The caller uses the id to select its capability-specific provider
// map.
func (c *Catalog) resolve(spec string) (ai.Model, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if m, ok := c.models[spec]; ok {
		providerID, _, _ := strings.Cut(spec, "/")
		return m, providerID, nil
	}
	return c.resolveBareLocked(spec)
}

// resolveBareLocked resolves a spec that has no provider prefix. It scans
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

// LanguageModel resolves a spec to a bound [ai.LanguageModel]. If the spec
// is unknown, or the provider does not support text generation, it returns
// an error.
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

// StreamText resolves spec and streams a text response. There is no separate
// error return. A resolution error reaches the caller on the returned stream,
// through [ai.ErrStream]. Wait() blocks for the final message.
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

// GenerateText resolves spec and blocks for a text response. It is a
// convenience wrapper around StreamText(...).Wait().
func (c *Catalog) GenerateText(
	ctx context.Context,
	spec string,
	p ai.Prompt,
	opts ...ai.Option,
) (*ai.Message, error) {
	return c.StreamText(ctx, spec, p, opts...).Wait()
}

// ImageModel resolves a spec to a bound [ai.ImageModel]. If the spec is
// unknown, or the provider does not support image generation, it returns
// an error.
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

// SpeechModel resolves a spec to a bound [ai.SpeechModel]. If the spec is
// unknown, or the provider does not support speech generation, it returns
// an error.
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

// Agent builds an agent for a "<kind>/<model>" spec. If a custom factory is
// registered for the kind, Agent uses it. If not, the spec resolves to a
// language model, and Agent wraps it in the default [agent.New] loop.
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
