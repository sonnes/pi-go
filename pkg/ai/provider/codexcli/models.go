package codexcli

import "github.com/sonnes/pi-go/pkg/ai"

// Model-info vars are pure metadata — no credentials, no provider
// identity. The Codex CLI accepts an arbitrary model via [WithModel]
// or the per-call [ai.Model.ID], so this is a small representative
// list rather than an exhaustive catalog. Bind one to the provider
// with [Provider.LanguageModel] or [ai.NewLanguageModel].
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

// models is the representative catalog this provider serves.
var models = []ai.Model{GPT5Codex}

// Models returns the models served by the Codex CLI provider.
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
