package ai_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestProviderRegistry(t *testing.T) {
	// Clean slate.
	ai.ClearProviders()
	defer ai.ClearProviders()

	_, ok := ai.GetProvider("test-api")
	assert.False(t, ok, "should not find unregistered provider")

	p := &fakeProvider{api: "test-api"}
	ai.RegisterProvider("test-api", p)

	got, ok := ai.GetProvider("test-api")
	require.True(t, ok)
	assert.Equal(t, "test-api", got.API())

	all := ai.Providers()
	assert.Len(t, all, 1)

	ai.UnregisterProvider("test-api")
	_, ok = ai.GetProvider("test-api")
	assert.False(t, ok)
}

// fakeProvider is a test double for ai.Provider.
type fakeProvider struct {
	api     string
	message *ai.Message
}

func (f *fakeProvider) API() string { return f.api }

func (f *fakeProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	msg := f.message
	return ai.NewEventStream(func(push func(ai.Event)) {
		push(ai.Event{
			Type:    ai.EventDone,
			Message: msg,
		})
	})
}
