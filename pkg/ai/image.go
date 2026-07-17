package ai

import "context"

// ImageProvider is an optional capability interface for providers that
// support image generation. Bind it with [NewImageModel].
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

// ImageModel is a [Model] bound to an [ImageProvider]. Create one with
// [NewImageModel].
type ImageModel interface {
	Model() Model
	GenerateImage(ctx context.Context, p Prompt, opts ...Option) (*ImageResponse, error)
}

// NewImageModel binds model metadata to an image provider.
func NewImageModel(info Model, p ImageProvider) ImageModel {
	return imageModel{info: info, prov: p}
}

type imageModel struct {
	info Model
	prov ImageProvider
}

func (m imageModel) Model() Model { return m.info }

func (m imageModel) GenerateImage(ctx context.Context, p Prompt, opts ...Option) (*ImageResponse, error) {
	return m.prov.GenerateImage(ctx, m.info, p, ApplyOptions(opts))
}
