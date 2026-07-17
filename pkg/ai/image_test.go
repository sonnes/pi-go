package ai_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestNewImageModel_BindsAndForwards(t *testing.T) {
	p := &fakeImageProvider{}
	info := ai.Model{ID: "img-1"}

	im := ai.NewImageModel(info, p)

	assert.Equal(t, info, im.Model())

	resp, err := im.GenerateImage(
		context.Background(),
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("a cat")}},
	)
	require.NoError(t, err)
	require.Len(t, resp.Images, 1)
	assert.Equal(t, "image/png", resp.Images[0].MediaType)
	assert.Equal(t, info, p.gotModel)
}

// fakeImageProvider is a test double for ai.ImageProvider.
type fakeImageProvider struct {
	gotModel ai.Model
}

func (f *fakeImageProvider) GenerateImage(
	_ context.Context,
	model ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.ImageResponse, error) {
	f.gotModel = model
	return &ai.ImageResponse{
		Images: []ai.GeneratedImage{{MediaType: "image/png", Data: []byte{1, 2, 3}}},
	}, nil
}
