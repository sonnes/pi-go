package ai

import "context"

// SpeechProvider is an optional capability interface for providers that
// support speech (text-to-speech) generation. Bind it with [NewSpeechModel].
type SpeechProvider interface {
	GenerateSpeech(ctx context.Context, model Model, p Prompt, opts StreamOptions) (*SpeechResponse, error)
}

// SpeechResponse contains generated audio.
type SpeechResponse struct {
	Audio     []byte
	MediaType string
}

// SpeechModel is a [Model] bound to a [SpeechProvider]. Create one with
// [NewSpeechModel].
type SpeechModel interface {
	Model() Model
	GenerateSpeech(ctx context.Context, p Prompt, opts ...Option) (*SpeechResponse, error)
}

// NewSpeechModel binds model metadata to a speech provider.
func NewSpeechModel(info Model, p SpeechProvider) SpeechModel {
	return speechModel{info: info, prov: p}
}

type speechModel struct {
	info Model
	prov SpeechProvider
}

func (m speechModel) Model() Model { return m.info }

func (m speechModel) GenerateSpeech(ctx context.Context, p Prompt, opts ...Option) (*SpeechResponse, error) {
	return m.prov.GenerateSpeech(ctx, m.info, p, ApplyOptions(opts))
}
