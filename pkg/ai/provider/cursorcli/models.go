package cursorcli

import "github.com/sonnes/pi-go/pkg/ai"

// Model-info vars hold metadata only. They contain no credentials and no
// provider identity. The Cursor CLI accepts any model through [WithModel]
// or the per-call [ai.Model.ID]. This list is therefore a small sample,
// not a full catalog. Bind one model to the provider with
// [Provider.LanguageModel] or [ai.NewLanguageModel].
var (
	// CursorFast is the default fast model of Cursor.
	CursorFast = ai.Model{
		ID:       "cursor-fast",
		Name:     "Cursor Fast",
		ToolCall: true,
		Input:    []ai.Modality{ai.ModalityText},
		Output:   []ai.Modality{ai.ModalityText},
	}
)

// models is the sample catalog that this provider serves.
var models = []ai.Model{CursorFast}

// Models returns the models that the Cursor CLI provider serves.
func Models() []ai.Model {
	out := make([]ai.Model, len(models))
	copy(out, models)
	return out
}

// LanguageModel binds a model-info value to this provider and returns a
// callable [ai.LanguageModel]. It is sugar for [ai.NewLanguageModel].
func (p *Provider) LanguageModel(info ai.Model) ai.LanguageModel {
	return ai.NewLanguageModel(info, p)
}
