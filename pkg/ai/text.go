package ai

import "context"

// TextProvider is the core capability interface for streaming text
// generation. It is pure behavior. Identity ("who is this provider") is not
// part of the capability. A catalog supplies the identity when it registers
// the provider.
//
// Bind a [Model] to a TextProvider with [NewLanguageModel] to get a
// callable [LanguageModel].
type TextProvider interface {
	StreamText(ctx context.Context, model Model, p Prompt, opts StreamOptions) *EventStream
}

// LanguageModel is a [Model] bound to a [TextProvider]. It is the callable
// unit that agents and the generation helpers accept. Create one with
// [NewLanguageModel].
type LanguageModel interface {
	// Model returns the metadata of the bound model.
	Model() Model
	// StreamText streams a text response from the bound provider.
	StreamText(ctx context.Context, p Prompt, opts ...Option) *EventStream
}

// NewLanguageModel binds model metadata to a text provider. The StreamText
// method of the result fixes the model argument to info. The Model method
// returns info verbatim.
func NewLanguageModel(info Model, p TextProvider) LanguageModel {
	return languageModel{info: info, prov: p}
}

// languageModel is the default [LanguageModel] implementation. It is a thin
// binding that fixes the model argument on each provider call. When the
// bound provider also implements [ObjectProvider], the model satisfies
// [ObjectModel], which [GenerateObject] upgrades to.
type languageModel struct {
	info Model
	prov TextProvider
}

func (m languageModel) Model() Model { return m.info }

func (m languageModel) StreamText(ctx context.Context, p Prompt, opts ...Option) *EventStream {
	return m.prov.StreamText(ctx, m.info, p, ApplyOptions(opts))
}
