package pi_test

import (
	"context"
	"testing"

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

func TestGenerate_ViaDefaultCatalog(t *testing.T) {
	pi.Default.RegisterProvider(fakeProvider{})

	msg, err := pi.Generate(
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

func TestModelVarsAreReExported(t *testing.T) {
	// Re-exported metadata is usable without a provider.
	assert.Equal(t, "claude-sonnet-4-6", pi.ClaudeSonnet.ID)
	assert.NotEmpty(t, pi.ClaudeOpus.ID)
}
