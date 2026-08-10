package pi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
)

type detectedProvider struct{}

func (detectedProvider) StreamText(
	context.Context,
	ai.Model,
	ai.Prompt,
	ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(func(ai.Event)) (*ai.Message, error) {
		return &ai.Message{Role: ai.RoleAssistant}, nil
	})
}

func (detectedProvider) GenerateImage(
	context.Context,
	ai.Model,
	ai.Prompt,
	ai.StreamOptions,
) (*ai.ImageResponse, error) {
	return &ai.ImageResponse{}, nil
}

func (detectedProvider) GenerateSpeech(
	context.Context,
	ai.Model,
	ai.Prompt,
	ai.StreamOptions,
) (*ai.SpeechResponse, error) {
	return &ai.SpeechResponse{}, nil
}

func TestRegisterDetectedRegistersEveryCapability(t *testing.T) {
	c := catalog.New()
	d := Detector{
		ProviderID: "detected",
		Models:     []ai.Model{{ID: "m1"}},
	}

	registerDetected(c, d, detectedProvider{})

	_, err := c.LanguageModel("detected/m1")
	require.NoError(t, err)
	_, err = c.ImageModel("detected/m1")
	require.NoError(t, err)
	_, err = c.SpeechModel("detected/m1")
	require.NoError(t, err)
}
