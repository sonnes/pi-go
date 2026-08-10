package pi_test

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/pi"
)

// fakeProvider implements ai.TextProvider.
type fakeProvider struct{}

func (fakeProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
		return &ai.Message{Role: ai.RoleAssistant, Content: []ai.Content{ai.Text{Text: "ok"}}}, nil
	})
}

func TestGenerateText_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterTextProvider("fake", fakeProvider{}, ai.Model{ID: "m1"})

	msg, err := pi.GenerateText(
		context.Background(),
		"fake/m1",
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	)
	require.NoError(t, err)
	assert.Equal(t, "ok", msg.Content[0].(ai.Text).Text)
}

func TestStreamText_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterTextProvider("fake", fakeProvider{}, ai.Model{ID: "m1"})

	msg, err := pi.StreamText(
		context.Background(),
		"fake/m1",
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	).Wait()
	require.NoError(t, err)
	assert.Equal(t, "ok", msg.Content[0].(ai.Text).Text)
}

func TestGenerateImage_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterImageProvider("img", fakeImageProvider{}, ai.Model{ID: "m1"})

	resp, err := pi.GenerateImage(context.Background(), "img/m1", ai.Prompt{})
	require.NoError(t, err)
	require.Len(t, resp.Images, 1)
	assert.Equal(t, "image/png", resp.Images[0].MediaType)
}

// fakeImageProvider implements ai.ImageProvider.
type fakeImageProvider struct{}

func (fakeImageProvider) GenerateImage(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.ImageResponse, error) {
	return &ai.ImageResponse{Images: []ai.GeneratedImage{{MediaType: "image/png"}}}, nil
}

// fakeObjectProvider implements ai.TextProvider + ai.ObjectProvider.
type fakeObjectProvider struct{}

func (fakeObjectProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
		return &ai.Message{Role: ai.RoleAssistant}, nil
	})
}

func (fakeObjectProvider) GenerateObject(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ *jsonschema.Schema,
	_ ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	return &ai.ObjectResponse{Raw: `{"x":3}`}, nil
}

func TestGenerateObject_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterTextProvider("obj", fakeObjectProvider{}, ai.Model{ID: "m1"})

	type point struct {
		X int `json:"x"`
	}
	res, err := pi.GenerateObject[point](context.Background(), "obj/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, 3, res.Object.X)
}

// fakeSpeechProvider implements ai.SpeechProvider.
type fakeSpeechProvider struct{}

func (fakeSpeechProvider) GenerateSpeech(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.SpeechResponse, error) {
	return &ai.SpeechResponse{Audio: []byte{1}, MediaType: "audio/mp3"}, nil
}

func TestGenerateSpeech_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterSpeechProvider("tts", fakeSpeechProvider{}, ai.Model{ID: "m1"})

	resp, err := pi.GenerateSpeech(context.Background(), "tts/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, "audio/mp3", resp.MediaType)
}
