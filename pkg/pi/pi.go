// Package pi is the batteries-included front door to the pi SDK. It owns a
// default [catalog.Catalog]. On first use it auto-wires providers from the
// environment credentials. It also re-exports the common types and model
// vars, so a single import covers the happy path.
//
// Use the lower layers directly for explicit control, such as multiple
// credentials, no globals, or custom base URLs. Build a provider with
// anthropic.New, bind it with [ai.NewLanguageModel], and register it in
// your own [catalog.Catalog].
package pi

import (
	"context"
	"sync"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/durable"
)

// Default is the process-wide catalog behind the package-level helpers.
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

// ensureProviders auto-wires the default catalog one time. It runs the
// detection chain and registers every provider that has credentials. The
// detection lives in detect.go. Each provider owns its own [Detector].
func ensureProviders() {
	once.Do(func() {
		for _, d := range detectors {
			if p, ok := d.Detect(); ok {
				registerDetected(Default, d, p)
			}
		}
	})
}

// StreamText resolves a "<provider>/<model>" spec against the default
// catalog and streams a text response. A bare model ID also works when
// exactly one registered provider serves it. A resolution error surfaces on
// the stream. Wait() blocks for the final message.
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
// a typed object. The provider of the model must support object generation.
func GenerateObject[T any](ctx context.Context, spec string, p Prompt, opts ...ai.Option) (*ai.ObjectResult[T], error) {
	ensureProviders()
	return catalog.GenerateObject[T](ctx, Default, spec, p, opts...)
}

// GenerateImage resolves spec and generates images from the prompt. The
// provider of the model must support image generation.
func GenerateImage(ctx context.Context, spec string, p Prompt, opts ...ai.Option) (*ai.ImageResponse, error) {
	ensureProviders()
	return Default.GenerateImage(ctx, spec, p, opts...)
}

// GenerateSpeech resolves spec and generates audio from the prompt. The
// provider of the model must support speech generation.
func GenerateSpeech(ctx context.Context, spec string, p Prompt, opts ...ai.Option) (*ai.SpeechResponse, error) {
	ensureProviders()
	return Default.GenerateSpeech(ctx, spec, p, opts...)
}

// Agent builds an agent for a "<kind>/<model>" spec from the default catalog.
func Agent(spec string, opts ...agent.Option) (agent.Agent, error) {
	ensureProviders()
	return Default.Agent(spec, opts...)
}

// DurableAgent builds a session-backed durable agent for a "<kind>/<model>"
// spec from the default catalog. Pass the durable options
// ([durable.WithStore], [durable.WithSessionID]) with the agent options.
func DurableAgent(ctx context.Context, spec string, opts ...agent.Option) (*durable.Agent, error) {
	ensureProviders()
	return Default.DurableAgent(ctx, spec, opts...)
}
