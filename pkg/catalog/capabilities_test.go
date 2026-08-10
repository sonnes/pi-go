package catalog_test

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
)

// --- image ---

type fakeImageProvider struct{}

func (fakeImageProvider) GenerateImage(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.ImageResponse, error) {
	return &ai.ImageResponse{Images: []ai.GeneratedImage{{MediaType: "image/png"}}}, nil
}

func TestImageModel_ResolvesAndBinds(t *testing.T) {
	c := catalog.New()
	c.RegisterImageProvider("img", fakeImageProvider{}, ai.Model{ID: "m1"})

	im, err := c.ImageModel("img/m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", im.Model().ID)

	resp, err := c.GenerateImage(context.Background(), "img/m1", ai.Prompt{})
	require.NoError(t, err)
	require.Len(t, resp.Images, 1)
	assert.Equal(t, "image/png", resp.Images[0].MediaType)
}

func TestImageModel_Unsupported(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("fake", &fakeProvider{}, textModels...) // text-only

	_, err := c.ImageModel("fake/m1")
	assert.ErrorContains(t, err, "does not support image generation")
}

// --- speech ---

type fakeSpeechProvider struct{}

func (fakeSpeechProvider) GenerateSpeech(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.SpeechResponse, error) {
	return &ai.SpeechResponse{Audio: []byte{1}, MediaType: "audio/mp3"}, nil
}

func TestSpeechModel_ResolvesAndBinds(t *testing.T) {
	c := catalog.New()
	c.RegisterSpeechProvider("tts", fakeSpeechProvider{}, ai.Model{ID: "m1"})

	sm, err := c.SpeechModel("tts/m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", sm.Model().ID)

	resp, err := c.GenerateSpeech(context.Background(), "tts/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, "audio/mp3", resp.MediaType)
}

func TestSpeechModel_Unsupported(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("fake", &fakeProvider{}, textModels...) // text-only

	_, err := c.SpeechModel("fake/m1")
	assert.ErrorContains(t, err, "does not support speech generation")
}

// --- object ---

type fakeObjectProvider struct{ raw string }

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

func (f fakeObjectProvider) GenerateObject(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ *jsonschema.Schema,
	_ ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	return &ai.ObjectResponse{Raw: f.raw}, nil
}

type point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func TestGenerateObject_ViaCatalog(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider(
		"obj",
		fakeObjectProvider{raw: `{"x":3,"y":4}`},
		ai.Model{ID: "m1"},
	)

	res, err := catalog.GenerateObject[point](context.Background(), c, "obj/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, point{X: 3, Y: 4}, res.Object)
}

func TestGenerateObject_Unsupported(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("fake", &fakeProvider{}, textModels...) // no ObjectProvider

	_, err := catalog.GenerateObject[point](context.Background(), c, "fake/m1", ai.Prompt{})
	assert.ErrorContains(t, err, "does not support object generation")
}

func TestCapabilityProvidersShareModelsButNotProviderLookup(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("shared", &fakeProvider{})
	c.RegisterImageProvider(
		"shared",
		fakeImageProvider{},
		ai.Model{ID: "m1"},
	)

	_, err := c.LanguageModel("shared/m1")
	require.NoError(t, err, "image registration adds to the shared model index")

	_, err = c.ImageModel("shared/m1")
	require.NoError(t, err)

	c.RegisterSpeechProvider("shared", fakeSpeechProvider{})
	_, err = c.SpeechModel("shared/m1")
	require.NoError(t, err)
}
