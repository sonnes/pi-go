package cursorcli

import "github.com/sonnes/pi-go/pkg/ai"

// Model-info vars are pure metadata — no credentials, no provider
// identity. The Cursor CLI accepts an arbitrary model via [WithModel]
// or the per-call [ai.Model.ID], so this is a small representative
// list rather than an exhaustive catalog. Bind one to the provider
// with [Provider.LanguageModel] or [ai.NewLanguageModel].
var (
	// CursorFast is Cursor's default fast model.
	CursorFast = ai.Model{
		ID:       "cursor-fast",
		Name:     "Cursor Fast",
		ToolCall: true,
		Input:    []ai.Modality{ai.ModalityText},
		Output:   []ai.Modality{ai.ModalityText},
	}
)

// models is the representative catalog this provider serves.
var models = []ai.Model{CursorFast}

// Models returns the models served by the Cursor CLI provider.
func (p *Provider) Models() []ai.Model {
	out := make([]ai.Model, len(models))
	copy(out, models)
	return out
}

// LanguageModel binds a model-info value to this provider, producing a
// callable [ai.LanguageModel]. Sugar for [ai.NewLanguageModel].
func (p *Provider) LanguageModel(info ai.Model) ai.LanguageModel {
	return ai.NewLanguageModel(info, p)
}
