package ai

import "context"

// TextProvider is the core capability interface for streaming text
// generation. It is pure behavior: identity ("who is this provider") is
// not part of the capability and lives on the registration interface
// (catalog.Provider) instead.
//
// Bind a [Model] to a TextProvider with [NewLanguageModel] to get a
// callable [LanguageModel].
type TextProvider interface {
	StreamText(ctx context.Context, model Model, p Prompt, opts StreamOptions) *EventStream
}

// LanguageModel is a [Model] bound to a [TextProvider] — the callable
// unit. It is what agents and the generation helpers accept. Create one
// with [NewLanguageModel].
type LanguageModel interface {
	// Model returns the bound model's metadata.
	Model() Model
	// StreamText streams a text response from the bound provider.
	StreamText(ctx context.Context, p Prompt, opts ...Option) *EventStream
}

// NewLanguageModel binds model metadata to a text provider. The result's
// StreamText fixes the model argument to info; Model returns info verbatim.
func NewLanguageModel(info Model, p TextProvider) LanguageModel {
	return languageModel{info: info, prov: p}
}

// languageModel is the default [LanguageModel] implementation: a thin
// binding that fixes the model argument on each provider call. When the
// bound provider also implements [ObjectProvider], the model satisfies the
// unexported objectModel interface that [GenerateObject] upgrades to.
type languageModel struct {
	info Model
	prov TextProvider
}

func (m languageModel) Model() Model { return m.info }

func (m languageModel) StreamText(ctx context.Context, p Prompt, opts ...Option) *EventStream {
	return m.prov.StreamText(ctx, m.info, p, ApplyOptions(opts))
}
