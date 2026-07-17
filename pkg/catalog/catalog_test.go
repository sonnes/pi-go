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

func TestLanguageModel_BareModelID(t *testing.T) {
	c := catalog.New()
	c.RegisterProvider(&fakeProvider{id: "fake"})

	// A spec without a provider prefix resolves when exactly one
	// registered provider serves the model.
	lm, err := c.LanguageModel("m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", lm.Model().ID)

	// Bare aliases resolve too.
	_, err = c.LanguageModel("latest")
	require.NoError(t, err)
}

func TestLanguageModel_BareModelID_Ambiguous(t *testing.T) {
	c := catalog.New()
	c.RegisterProvider(&fakeProvider{id: "alpha"})
	c.RegisterProvider(&fakeProvider{id: "beta"})

	_, err := c.LanguageModel("m1")
	require.Error(t, err)
	assert.ErrorContains(t, err, "ambiguous")
	// The error names the full specs so the caller can disambiguate.
	assert.ErrorContains(t, err, "alpha/m1")
	assert.ErrorContains(t, err, "beta/m1")
}

func TestGenerateText_ViaCatalog(t *testing.T) {
	c := catalog.New()
	c.RegisterProvider(&fakeProvider{id: "fake"})

	msg, err := c.GenerateText(context.Background(), "fake/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, ai.RoleAssistant, msg.Role)
}

func TestGenerateText_Errors(t *testing.T) {
	c := catalog.New()

	_, err := c.GenerateText(context.Background(), "nope/m1", ai.Prompt{})
	assert.ErrorContains(t, err, "unknown model")
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
