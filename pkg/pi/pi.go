// Package pi is the batteries-included front door to the pi SDK. It owns
// a default [catalog.Catalog], auto-wires providers from environment
// credentials on first use, and re-exports the common types and model
// vars so a single import covers the happy path.
//
// For explicit control (multiple credentials, no globals, custom base
// URLs) use the lower layers directly: construct a provider with e.g.
// anthropic.New, bind with [ai.NewLanguageModel], and register with your
// own [catalog.Catalog].
package pi

import (
	"context"
	"sync"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
)

// Default is the process-wide catalog backing the package-level helpers.
var Default = catalog.New()

// Type aliases so callers need only import pi.
type (
	Model         = ai.Model
	LanguageModel = ai.LanguageModel
	Prompt        = ai.Prompt
	Message       = ai.Message
	Catalog       = catalog.Catalog
)

// Agent option re-exports for the common cases.
var (
	WithTools        = agent.WithTools
	WithMaxTurns     = agent.WithMaxTurns
	WithSystemPrompt = agent.WithSystemPrompt
)

var once sync.Once

// ensureProviders auto-wires the default catalog once by running the
// detection chain and registering every provider whose credentials are
// present. Detection lives in detect.go; each provider owns its own
// [Detector].
func ensureProviders() {
	once.Do(func() {
		for _, d := range detectors {
			if p, ok := d.Detect(); ok {
				Default.RegisterProvider(p)
			}
		}
	})
}

// StreamText resolves a "<provider>/<model>" spec — or a bare model ID
// when exactly one registered provider serves it — against the default
// catalog and streams a text response. A resolution error surfaces on the
// stream; block for the final message with Wait().
func StreamText(ctx context.Context, spec string, p Prompt, opts ...ai.Option) *ai.EventStream {
	ensureProviders()
	return Default.StreamText(ctx, spec, p, opts...)
}

// GenerateText resolves spec and blocks for a text response. Convenience
// wrapper around StreamText(...).Wait().
func GenerateText(ctx context.Context, spec string, p Prompt, opts ...ai.Option) (*Message, error) {
	return StreamText(ctx, spec, p, opts...).Wait()
}

// GenerateObject resolves spec against the default catalog and generates
// a typed object. The model's provider must support object generation.
func GenerateObject[T any](ctx context.Context, spec string, p Prompt, opts ...ai.Option) (*ai.ObjectResult[T], error) {
	ensureProviders()
	return catalog.GenerateObject[T](ctx, Default, spec, p, opts...)
}

// GenerateImage resolves spec and generates images from the prompt. The
// model's provider must support image generation.
func GenerateImage(ctx context.Context, spec string, p Prompt, opts ...ai.Option) (*ai.ImageResponse, error) {
	ensureProviders()
	return Default.GenerateImage(ctx, spec, p, opts...)
}

// GenerateSpeech resolves spec and generates audio from the prompt. The
// model's provider must support speech generation.
func GenerateSpeech(ctx context.Context, spec string, p Prompt, opts ...ai.Option) (*ai.SpeechResponse, error) {
	ensureProviders()
	return Default.GenerateSpeech(ctx, spec, p, opts...)
}

// Agent builds an agent for a "<kind>/<model>" spec from the default catalog.
func Agent(spec string, opts ...agent.Option) (agent.Agent, error) {
	ensureProviders()
	return Default.Agent(spec, opts...)
}
