package ai

import (
	"context"
	"fmt"
)

// Generate resolves a "<provider>/<model>" spec from the default registry and
// generates a text response synchronously.
func Generate(ctx context.Context, spec string, p Prompt, opts ...Option) (*Message, error) {
	m, ok := ResolveModel(spec)
	if !ok {
		return nil, fmt.Errorf("ai: unknown model %q", spec)
	}
	return GenerateText(ctx, m, p, opts...)
}

// Stream resolves a "<provider>/<model>" spec from the default registry and
// streams a text response.
func Stream(ctx context.Context, spec string, p Prompt, opts ...Option) *EventStream {
	m, ok := ResolveModel(spec)
	if !ok {
		return errStream(fmt.Errorf("ai: unknown model %q", spec))
	}
	return StreamText(ctx, m, p, opts...)
}

// GenerateObject resolves a "<provider>/<model>" spec from the default registry
// and generates a typed object. T must be JSON-deserializable.
func GenerateObject[T any](ctx context.Context, spec string, p Prompt, opts ...Option) (*ObjectResult[T], error) {
	m, ok := ResolveModel(spec)
	if !ok {
		return nil, fmt.Errorf("ai: unknown model %q", spec)
	}
	return generateObject[T](ctx, m, p, opts...)
}

// GenerateImage resolves a "<provider>/<model>" spec from the default registry
// and generates images from the prompt.
func GenerateImage(ctx context.Context, spec string, p Prompt, opts ...Option) (*ImageResponse, error) {
	m, ok := ResolveModel(spec)
	if !ok {
		return nil, fmt.Errorf("ai: unknown model %q", spec)
	}
	return generateImage(ctx, m, p, opts...)
}
