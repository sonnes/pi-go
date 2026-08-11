package codexcli

import "github.com/sonnes/pi-go/pkg/ai"

// Model-info vars hold metadata only. They contain no credentials and no
// provider identity. The Codex CLI accepts any model through [WithModel]
// or the per-call [ai.Model.ID]. This list is therefore a small sample,
// not a full catalog. Bind one model to the provider with
// [Provider.LanguageModel] or [ai.NewLanguageModel].
var (
	// GPT5Codex is the default Codex model.
	GPT5Codex = ai.Model{
		ID:        "gpt-5-codex",
		Name:      "GPT-5 Codex",
		Reasoning: true,
		ToolCall:  true,
		Input:     []ai.Modality{ai.ModalityText},
		Output:    []ai.Modality{ai.ModalityText},
	}
)

// models is the sample catalog that this provider serves.
var models = []ai.Model{GPT5Codex}

// Models returns the models that the Codex CLI provider serves.
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
