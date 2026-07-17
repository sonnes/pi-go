package ai_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestNewLanguageModel_BindsInfoAndForwards(t *testing.T) {
	p := &fakeProvider{
		api:     "fake",
		message: &ai.Message{Role: ai.RoleAssistant},
	}
	info := ai.Model{ID: "m-1", Name: "Model One"}

	lm := ai.NewLanguageModel(info, p)

	// Model() returns the metadata verbatim.
	assert.Equal(t, info, lm.Model())

	// StreamText forwards the bound model to the provider.
	msg, err := lm.StreamText(
		context.Background(),
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	).Wait()
	require.NoError(t, err)
	assert.Equal(t, ai.RoleAssistant, msg.Role)
	assert.Equal(t, info, p.gotModel)
}

func TestGenerateText_WithFakeProvider(t *testing.T) {
	expected := &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.Content{ai.Text{Text: "response"}},
	}

	p := &fakeProvider{
		api:     "fake",
		message: expected,
	}
	lm := ai.NewLanguageModel(ai.Model{ID: "fake-1"}, p)

	msg, err := ai.GenerateText(
		context.Background(),
		lm,
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	)
	require.NoError(t, err)
	assert.Equal(t, expected, msg)
}

// fakeProvider is a test double for ai.TextProvider. It records the model
// it was called with and returns a canned message.
type fakeProvider struct {
	api      string
	message  *ai.Message
	gotModel ai.Model
}

func (f *fakeProvider) ID() string { return f.api }

func (f *fakeProvider) StreamText(
	_ context.Context,
	model ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	f.gotModel = model
	msg := f.message
	return ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
		return msg, nil
	})
}
