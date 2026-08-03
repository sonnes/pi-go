package ai_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestCountTokens_UpgradesTextModel(t *testing.T) {
	p := &fakeTokenCounter{count: 42}
	lm := ai.NewLanguageModel(ai.Model{ID: "count-1"}, p)

	count, err := ai.CountTokens(
		context.Background(),
		lm,
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hello")}},
	)
	require.NoError(t, err)
	assert.Equal(t, 42, count.Total)
}

func TestCountTokens_UnsupportedProvider(t *testing.T) {
	lm := ai.NewLanguageModel(ai.Model{ID: "x"}, &fakeProvider{api: "fake"})

	_, err := ai.CountTokens(context.Background(), lm, ai.Prompt{})
	assert.ErrorContains(t, err, "does not support token counting")
}

type fakeTokenCounter struct {
	count int
}

func (f *fakeTokenCounter) StreamText(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
		return &ai.Message{Role: ai.RoleAssistant}, nil
	})
}

func (f *fakeTokenCounter) CountTokens(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.TokenCount, error) {
	return &ai.TokenCount{Total: f.count}, nil
}
