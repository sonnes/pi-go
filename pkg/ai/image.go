package ai

import (
	"context"
	"fmt"
)

// ImageProvider is an optional interface for providers that support
// image generation.
type ImageProvider interface {
	GenerateImage(ctx context.Context, model Model, p Prompt, opts StreamOptions) (*ImageResponse, error)
}

// ImageResponse contains the generated images.
type ImageResponse struct {
	Images []GeneratedImage
}

// GeneratedImage represents a single generated image.
type GeneratedImage struct {
	Data      []byte
	MediaType string
	URL       string
}

// generateImage generates images from a resolved model. The exported,
// spec-based entry point is [GenerateImage].
func generateImage(ctx context.Context, model Model, p Prompt, opts ...Option) (*ImageResponse, error) {
	prov, ok := GetProvider(model.Provider)
	if !ok {
		return nil, fmt.Errorf("ai: no provider registered for %q", model.Provider)
	}
	ip, ok := prov.(ImageProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %q does not support image generation", model.Provider)
	}

	o := ApplyOptions(opts)
	return ip.GenerateImage(ctx, model, p, o)
}
