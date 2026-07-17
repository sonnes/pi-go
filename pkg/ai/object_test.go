package ai_test

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

type person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestGenerateObject_UpgradesTextModel(t *testing.T) {
	p := &fakeObjectProvider{raw: `{"name":"Ada","age":36}`}
	lm := ai.NewLanguageModel(ai.Model{ID: "obj-1"}, p)

	res, err := ai.GenerateObject[person](
		context.Background(),
		lm,
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("describe Ada")}},
	)
	require.NoError(t, err)
	assert.Equal(t, "Ada", res.Object.Name)
	assert.Equal(t, 36, res.Object.Age)
}

func TestGenerateObject_UnsupportedProvider(t *testing.T) {
	// fakeProvider (from text_test.go) is text-only; it does not
	// implement ai.ObjectProvider, so the upgrade fails.
	lm := ai.NewLanguageModel(ai.Model{ID: "x"}, &fakeProvider{api: "fake"})

	_, err := ai.GenerateObject[person](context.Background(), lm, ai.Prompt{})
	assert.ErrorContains(t, err, "does not support object generation")
}

// fakeObjectProvider implements ai.TextProvider (so it can be bound) and
// ai.ObjectProvider (so the bound model upgrades for object generation).
type fakeObjectProvider struct {
	raw string
}

func (f *fakeObjectProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
		return &ai.Message{Role: ai.RoleAssistant}, nil
	})
}

func (f *fakeObjectProvider) GenerateObject(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ *jsonschema.Schema,
	_ ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	return &ai.ObjectResponse{Raw: f.raw}, nil
}
