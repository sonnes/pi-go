package ai_test

import (
	"context"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func opus() ai.Model {
	return ai.Model{
		Provider: "anthropic-messages",
		ID:       "claude-opus-4-8",
		Aliases:  []string{"opus"},
	}
}

func TestRegistry_Models(t *testing.T) {
	r := ai.NewRegistry()
	r.RegisterModel(opus())

	got, ok := r.ResolveModel("anthropic-messages/claude-opus-4-8")
	require.True(t, ok)
	assert.Equal(t, "claude-opus-4-8", got.ID)

	got, ok = r.ResolveModel("anthropic-messages/opus") // alias spec
	require.True(t, ok)
	assert.Equal(t, "claude-opus-4-8", got.ID)

	_, ok = r.ResolveModel("anthropic-messages/nope")
	assert.False(t, ok)

	assert.Len(t, r.Models(), 1) // deduped across alias

	r.UnregisterModel("anthropic-messages/opus") // remove via alias
	_, ok = r.ResolveModel("anthropic-messages/claude-opus-4-8")
	assert.False(t, ok)
	assert.Empty(t, r.Models())
}

func TestRegistry_ReregisterClearsStaleAliases(t *testing.T) {
	r := ai.NewRegistry()
	r.RegisterModel(ai.Model{Provider: "x", ID: "foo", Aliases: []string{"a", "b"}})

	// Re-register the same model with one alias dropped.
	r.RegisterModel(ai.Model{Provider: "x", ID: "foo", Aliases: []string{"a"}})

	_, ok := r.ResolveModel("x/b")
	assert.False(t, ok, "dropped alias must not resolve after re-register")

	_, ok = r.ResolveModel("x/a")
	assert.True(t, ok, "retained alias must still resolve")

	assert.Len(t, r.Models(), 1)
}

func TestGenerate_ResolvesSpec(t *testing.T) {
	ai.ClearProviders()
	ai.ClearModels()
	defer func() { ai.ClearProviders(); ai.ClearModels() }()

	want := &ai.Message{Role: ai.RoleAssistant, Content: []ai.Content{ai.Text{Text: "hi"}}}
	ai.RegisterProvider("anthropic-messages", &fakeProvider{api: "anthropic-messages", message: want})
	ai.RegisterModel(opus())

	got, err := ai.Generate(
		context.Background(),
		"anthropic-messages/claude-opus-4-8",
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
	)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = ai.Generate(context.Background(), "anthropic-messages/unknown", ai.Prompt{})
	assert.ErrorContains(t, err, "unknown model")
}
