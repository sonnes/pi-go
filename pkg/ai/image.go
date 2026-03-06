package ai

import (
	"context"
	"fmt"
)

// ImageProvider is an optional interface for providers that support
// image generation.
type ImageProvider interface {
	GenerateImage(ctx context.Context, model Model, req *ImageRequest) (*ImageResponse, error)
}

// ImageRequest holds inputs for image generation.
type ImageRequest struct {
	Prompt  string
	Size    string // e.g. "1024x1024"
	N       int    // number of images to generate
	Options StreamOptions
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

// GenerateImage generates images from a text prompt.
func GenerateImage(ctx context.Context, model Model, prompt string, opts ...Option) (*ImageResponse, error) {
	p, ok := GetProvider(model.API)
	if !ok {
		return nil, fmt.Errorf("ai: no provider registered for API %q", model.API)
	}
	ip, ok := p.(ImageProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider for API %q does not support image generation", model.API)
	}

	o := ApplyOptions(opts)
	return ip.GenerateImage(ctx, model, &ImageRequest{
		Prompt:  prompt,
		Options: o,
	})
}
