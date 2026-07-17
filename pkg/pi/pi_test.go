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

// fakeProvider implements catalog.Provider + ai.TextProvider.
type fakeProvider struct{}

func (fakeProvider) Provider() string   { return "fake" }
func (fakeProvider) Models() []ai.Model { return []ai.Model{{ID: "m1"}} }

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
	pi.Default.RegisterProvider(fakeProvider{})

	msg, err := pi.GenerateText(
		context.Background(),
		"fake/m1",
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	)
	require.NoError(t, err)
	assert.Equal(t, "ok", msg.Content[0].(ai.Text).Text)
}

func TestStreamText_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterProvider(fakeProvider{})

	msg, err := pi.StreamText(
		context.Background(),
		"fake/m1",
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	).Wait()
	require.NoError(t, err)
	assert.Equal(t, "ok", msg.Content[0].(ai.Text).Text)
}

func TestGenerateImage_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterProvider(fakeImageProvider{})

	resp, err := pi.GenerateImage(context.Background(), "img/m1", ai.Prompt{})
	require.NoError(t, err)
	require.Len(t, resp.Images, 1)
	assert.Equal(t, "image/png", resp.Images[0].MediaType)
}

// fakeImageProvider implements catalog.Provider + ai.ImageProvider.
type fakeImageProvider struct{}

func (fakeImageProvider) Provider() string   { return "img" }
func (fakeImageProvider) Models() []ai.Model { return []ai.Model{{ID: "m1"}} }

func (fakeImageProvider) GenerateImage(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.ImageResponse, error) {
	return &ai.ImageResponse{Images: []ai.GeneratedImage{{MediaType: "image/png"}}}, nil
}

// fakeObjectProvider implements catalog.Provider + ai.TextProvider +
// ai.ObjectProvider.
type fakeObjectProvider struct{}

func (fakeObjectProvider) Provider() string   { return "obj" }
func (fakeObjectProvider) Models() []ai.Model { return []ai.Model{{ID: "m1"}} }

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
	pi.Default.RegisterProvider(fakeObjectProvider{})

	type point struct {
		X int `json:"x"`
	}
	res, err := pi.GenerateObject[point](context.Background(), "obj/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, 3, res.Object.X)
}

// fakeSpeechProvider implements catalog.Provider + ai.SpeechProvider.
type fakeSpeechProvider struct{}

func (fakeSpeechProvider) Provider() string   { return "tts" }
func (fakeSpeechProvider) Models() []ai.Model { return []ai.Model{{ID: "m1"}} }

func (fakeSpeechProvider) GenerateSpeech(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.SpeechResponse, error) {
	return &ai.SpeechResponse{Audio: []byte{1}, MediaType: "audio/mp3"}, nil
}

func TestGenerateSpeech_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterProvider(fakeSpeechProvider{})

	resp, err := pi.GenerateSpeech(context.Background(), "tts/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, "audio/mp3", resp.MediaType)
}

func TestModelVarsAreReExported(t *testing.T) {
	// Re-exported metadata is usable without a provider.
	assert.Equal(t, "claude-sonnet-4-6", pi.ClaudeSonnet.ID)
	assert.NotEmpty(t, pi.ClaudeOpus.ID)
}
