package ai_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestNewSpeechModel_BindsAndForwards(t *testing.T) {
	p := &fakeSpeechProvider{}
	info := ai.Model{ID: "tts-1"}

	sm := ai.NewSpeechModel(info, p)

	assert.Equal(t, info, sm.Model())

	resp, err := sm.GenerateSpeech(
		context.Background(),
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hello")}},
	)
	require.NoError(t, err)
	assert.Equal(t, "audio/mpeg", resp.MediaType)
	assert.Equal(t, info, p.gotModel)
}

// fakeSpeechProvider is a test double for ai.SpeechProvider.
type fakeSpeechProvider struct {
	gotModel ai.Model
}

func (f *fakeSpeechProvider) GenerateSpeech(
	_ context.Context,
	model ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) (*ai.SpeechResponse, error) {
	f.gotModel = model
	return &ai.SpeechResponse{Audio: []byte{1, 2, 3}, MediaType: "audio/mpeg"}, nil
}
