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

// fakeProvider implements ai.TextProvider.
type fakeProvider struct{}

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

var textModels = []ai.Model{{ID: "m1", Aliases: []string{"latest"}}}

func TestLanguageModel_ResolvesAndBinds(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("fake", &fakeProvider{}, textModels...)

	lm, err := c.LanguageModel("fake/m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", lm.Model().ID)

	// Alias resolves too.
	_, err = c.LanguageModel("fake/latest")
	require.NoError(t, err)
}

func TestLanguageModel_Errors(t *testing.T) {
	c := catalog.New()
	c.RegisterModel("textless", ai.Model{ID: "x"})

	_, err := c.LanguageModel("nope/m1")
	assert.ErrorContains(t, err, "unknown model")

	_, err = c.LanguageModel("textless/x")
	assert.ErrorContains(t, err, "does not support text generation")
}

func TestLanguageModel_BareModelID(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("fake", &fakeProvider{}, textModels...)

	// A spec with no provider prefix resolves when exactly one registered
	// provider serves the model.
	lm, err := c.LanguageModel("m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", lm.Model().ID)

	// Bare aliases resolve too.
	_, err = c.LanguageModel("latest")
	require.NoError(t, err)
}

func TestLanguageModel_BareModelID_Ambiguous(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("alpha", &fakeProvider{}, textModels...)
	c.RegisterTextProvider("beta", &fakeProvider{}, textModels...)

	_, err := c.LanguageModel("m1")
	require.Error(t, err)
	assert.ErrorContains(t, err, "ambiguous")
	// The error names the full specs, so the caller can choose one.
	assert.ErrorContains(t, err, "alpha/m1")
	assert.ErrorContains(t, err, "beta/m1")
}

func TestGenerateText_ViaCatalog(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("fake", &fakeProvider{}, textModels...)

	msg, err := c.GenerateText(context.Background(), "fake/m1", ai.Prompt{})
	require.NoError(t, err)
	assert.Equal(t, ai.RoleAssistant, msg.Role)
}

func TestGenerateText_Errors(t *testing.T) {
	c := catalog.New()

	_, err := c.GenerateText(context.Background(), "nope/m1", ai.Prompt{})
	assert.ErrorContains(t, err, "unknown model")
}

func TestModel_RegisteredAndAgentKind(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("fake", &fakeProvider{}, textModels...)
	c.RegisterAgent("cli", func(_ string, _ ...agent.Option) (agent.Agent, error) {
		return nil, nil
	})

	// A registered spec returns its indexed metadata.
	m, err := c.Model("fake/m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", m.ID)

	// An alias resolves to the same model.
	m, err = c.Model("fake/latest")
	require.NoError(t, err)
	assert.Equal(t, "m1", m.ID)

	// A bare model ID resolves when one provider serves it.
	m, err = c.Model("m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", m.ID)

	// An agent kind owns its own catalog, so the model index has no
	// entry. Model synthesizes the metadata instead of failing.
	m, err = c.Model("cli/opus")
	require.NoError(t, err)
	assert.Equal(t, "opus", m.ID)
	assert.Equal(t, "opus", m.Name)

	// An unknown spec still fails, so a caller can validate eagerly.
	_, err = c.Model("nope/m1")
	assert.ErrorContains(t, err, "unknown model")
}

func TestAgent_DefaultAndCustom(t *testing.T) {
	c := catalog.New()
	c.RegisterTextProvider("fake", &fakeProvider{}, textModels...)

	// By default, any registered model becomes an agent.
	ag, err := c.Agent("fake/m1")
	require.NoError(t, err)
	require.NotNil(t, ag)

	// A custom kind wins over the default path.
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
