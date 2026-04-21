package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFactory returns a Factory that yields a fakeAgent carrying the marker.
// It lets registry tests exercise register/lookup without depending on Default
// construction or a real model.
func fakeFactory(marker string) Factory {
	return func(opts ...Option) Agent {
		return &fakeAgent{marker: marker}
	}
}

type fakeAgent struct {
	Agent
	marker string
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	const key = "reg-test-register-and-get"
	t.Cleanup(func() { UnregisterFactory(key) })

	RegisterFactory(key, fakeFactory("a"))

	got, ok := GetFactory(key)
	require.True(t, ok)

	fa := got().(*fakeAgent)
	assert.Equal(t, "a", fa.marker)
}

func TestRegistry_GetMissing(t *testing.T) {
	_, ok := GetFactory("reg-test-missing-definitely-not-registered")
	assert.False(t, ok)
}

func TestRegistry_Overwrite(t *testing.T) {
	const key = "reg-test-overwrite"
	t.Cleanup(func() { UnregisterFactory(key) })

	RegisterFactory(key, fakeFactory("first"))
	RegisterFactory(key, fakeFactory("second"))

	f, _ := GetFactory(key)
	fa := f().(*fakeAgent)
	assert.Equal(t, "second", fa.marker)
}

func TestRegistry_Unregister(t *testing.T) {
	const key = "reg-test-unregister"

	RegisterFactory(key, fakeFactory("a"))
	UnregisterFactory(key)

	_, ok := GetFactory(key)
	assert.False(t, ok)
}

func TestRegistry_FactoriesReturnsCopy(t *testing.T) {
	const key = "reg-test-copy"
	t.Cleanup(func() { UnregisterFactory(key) })

	RegisterFactory(key, fakeFactory("a"))

	snapshot := Factories()

	// Mutating the snapshot must not affect the registry.
	delete(snapshot, key)
	_, ok := GetFactory(key)
	assert.True(t, ok, "registry must not be affected by mutations to Factories() result")
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	const key = "reg-test-concurrent"
	t.Cleanup(func() { UnregisterFactory(key) })

	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			RegisterFactory(key, fakeFactory("m"))
		}()
		go func() {
			defer wg.Done()
			_, _ = GetFactory(key)
		}()
	}
	wg.Wait()

	_, ok := GetFactory(key)
	assert.True(t, ok)
}

func TestWithModel_SetsModel(t *testing.T) {
	model := ai.Model{ID: "test-model", API: "test-api"}

	cfg := ApplyOptions(WithModel(model))

	assert.Equal(t, model, cfg.Model)
}

func TestApplyOptions_CapturesFields(t *testing.T) {
	model := ai.Model{ID: "m"}
	cfg := ApplyOptions(
		WithModel(model),
		WithMaxTurns(5),
	)

	assert.Equal(t, model, cfg.Model)
	assert.Equal(t, 5, cfg.MaxTurns)
}

func TestWithModelName_SetsIDAndName(t *testing.T) {
	cfg := ApplyOptions(WithModelName("gpt-5"))
	assert.Equal(t, "gpt-5", cfg.Model.ID)
	assert.Equal(t, "gpt-5", cfg.Model.Name)
}

func TestWithModelName_PreservesOtherFieldsWhenFollowingWithModel(t *testing.T) {
	base := ai.Model{ID: "m", Name: "M", API: "some-api", Provider: "p"}
	cfg := ApplyOptions(WithModel(base), WithModelName("new-name"))

	assert.Equal(t, "new-name", cfg.Model.ID)
	assert.Equal(t, "new-name", cfg.Model.Name)
	assert.Equal(t, "some-api", cfg.Model.API, "non-identity fields must be preserved")
	assert.Equal(t, "p", cfg.Model.Provider)
}

func TestDefaultRun_ErrorsWhenModelMissing(t *testing.T) {
	a := New()
	defer a.Close()

	err := a.Send(context.Background(), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}
