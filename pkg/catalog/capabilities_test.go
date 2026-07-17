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

func TestImageModel_ResolvesAndBinds(t *testing.T) {
	c := catalog.New()
	c.RegisterProvider(fakeImageProvider{})

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
	c.RegisterProvider(&fakeProvider{id: "fake"}) // text-only

	_, err := c.ImageModel("fake/m1")
	assert.ErrorContains(t, err, "does not support image generation")
}

// --- object ---

type fakeObjectProvider struct{ raw string }

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
	c.RegisterProvider(fakeObjectProvider{raw: `{"x":3,"y":4}`})

	res, err := catalog.GenerateObject[point](context.Background(), c, "obj/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, point{X: 3, Y: 4}, res.Object)
}

func TestGenerateObject_Unsupported(t *testing.T) {
	c := catalog.New()
	c.RegisterProvider(&fakeProvider{id: "fake"}) // text-only, no ObjectProvider

	_, err := catalog.GenerateObject[point](context.Background(), c, "fake/m1", ai.Prompt{})
	assert.ErrorContains(t, err, "does not support object generation")
}
