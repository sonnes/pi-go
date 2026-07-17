package catalog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
)

// fakeProvider implements catalog.Provider and ai.TextProvider.
type fakeProvider struct{ id string }

func (f *fakeProvider) Provider() string { return f.id }

func (f *fakeProvider) Models() []ai.Model {
	return []ai.Model{{ID: "m1", Aliases: []string{"latest"}}}
}

func (f *fakeProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
		return &ai.Message{Role: ai.RoleAssistant}, nil
	})
}

// textlessProvider registers models but cannot generate text.
type textlessProvider struct{}

func (textlessProvider) Provider() string   { return "textless" }
func (textlessProvider) Models() []ai.Model { return []ai.Model{{ID: "x"}} }

func TestLanguageModel_ResolvesAndBinds(t *testing.T) {
	c := catalog.New()
	c.RegisterProvider(&fakeProvider{id: "fake"})

	lm, err := c.LanguageModel("fake/m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", lm.Model().ID)

	// Alias resolves too.
	_, err = c.LanguageModel("fake/latest")
	require.NoError(t, err)
}

func TestLanguageModel_Errors(t *testing.T) {
	c := catalog.New()
	c.RegisterProvider(textlessProvider{})

	_, err := c.LanguageModel("nope/m1")
	assert.ErrorContains(t, err, "unknown model")

	_, err = c.LanguageModel("textless/x")
	assert.ErrorContains(t, err, "does not support text generation")
}

func TestAgent_DefaultAndCustom(t *testing.T) {
	c := catalog.New()
	c.RegisterProvider(&fakeProvider{id: "fake"})

	// Default: any registered model becomes an agent.
	ag, err := c.Agent("fake/m1")
	require.NoError(t, err)
	require.NotNil(t, ag)

	// Custom kind wins over the default path.
	var called bool
	c.RegisterAgent("cli", func(spec string, _ ...agent.Option) (agent.Agent, error) {
		called = true
		assert.Equal(t, "cli/opus", spec)
		return ag, nil
	})
	_, err = c.Agent("cli/opus")
	require.NoError(t, err)
	assert.True(t, called)
}
