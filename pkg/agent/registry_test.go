package agent

import (
	"context"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCreate returns a CreateFunc yielding a fakeAgent carrying the marker and
// the model it was built with — enough to exercise register/lookup/Create
// without Default construction.
func fakeCreate(marker string) func(model ai.Model, opts ...Option) *fakeAgent {
	return func(model ai.Model, opts ...Option) *fakeAgent {
		return &fakeAgent{marker: marker, model: model}
	}
}

type fakeAgent struct {
	Agent
	marker string
	model  ai.Model
}

func TestRegisterAgent_AndGet(t *testing.T) {
	const key = "reg-test-get"
	t.Cleanup(func() { UnregisterAgent(key) })

	RegisterAgent(key, fakeCreate("a"))

	got, ok := GetAgent(key)
	require.True(t, ok)
	assert.Equal(t, "a", got(ai.Model{}).(*fakeAgent).marker)
}

func TestGetAgent_Missing(t *testing.T) {
	_, ok := GetAgent("definitely-not-registered")
	assert.False(t, ok)
}

func TestRegisterAgent_Overwrite(t *testing.T) {
	const key = "reg-test-overwrite"
	t.Cleanup(func() { UnregisterAgent(key) })

	RegisterAgent(key, fakeCreate("first"))
	RegisterAgent(key, fakeCreate("second"))

	f, _ := GetAgent(key)
	assert.Equal(t, "second", f(ai.Model{}).(*fakeAgent).marker)
}

func TestAgents_ReturnsCopy(t *testing.T) {
	const key = "reg-test-copy"
	t.Cleanup(func() { UnregisterAgent(key) })

	RegisterAgent(key, fakeCreate("a"))
	snapshot := Agents()
	delete(snapshot, key)

	_, ok := GetAgent(key)
	assert.True(t, ok, "registry must not be affected by mutating Agents() result")
}

func TestCreate_RoutesByPrefix(t *testing.T) {
	t.Cleanup(func() { UnregisterAgent("claude"); ClearModels() })

	RegisterAgent("claude", fakeCreate("cli"))

	a, err := Create("claude/sonnet")
	require.NoError(t, err)
	fa := a.(*fakeAgent)
	assert.Equal(t, "cli", fa.marker)
	assert.Equal(t, "claude", fa.model.Provider)
	assert.Equal(t, "sonnet", fa.model.ID) // bare model from the spec
}

func TestCreate_FallsBackToDefault(t *testing.T) {
	ai.ClearProviders()
	ai.ClearModels()
	t.Cleanup(func() { ai.ClearProviders(); ai.ClearModels() })

	ai.RegisterModel(ai.Model{Provider: "anthropic-messages", ID: "claude-sonnet-4-6"})

	a, err := Create("anthropic-messages/claude-sonnet-4-6")
	require.NoError(t, err)
	defer a.Close()
	_, isDefault := a.(*Default)
	assert.True(t, isDefault)
}

func TestCreate_UnknownModelErrors(t *testing.T) {
	ai.ClearModels()
	_, err := Create("anthropic-messages/nope")
	assert.ErrorContains(t, err, "unknown model")
}

func TestRegisterModel_ResolveModel(t *testing.T) {
	t.Cleanup(ClearModels)

	RegisterModel(ai.Model{Provider: "claude", ID: "sonnet"})
	m, ok := ResolveModel("claude/sonnet")
	require.True(t, ok)
	assert.Equal(t, "sonnet", m.ID)
}

func TestDefaultRun_ErrorsWhenModelMissing(t *testing.T) {
	a := New(ai.Model{})
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("hi")).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}
