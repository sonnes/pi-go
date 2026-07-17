package claudecli

import "github.com/sonnes/pi-go/pkg/ai"

// Model-info vars are pure metadata — no credentials, no provider
// identity. The Claude CLI accepts an arbitrary model via [WithModel]
// or the per-call [ai.Model.ID], so this is a small representative
// list rather than an exhaustive catalog. Bind one to the provider
// with [Provider.LanguageModel] or [ai.NewLanguageModel].
var (
	// ClaudeSonnet balances capability and cost.
	ClaudeSonnet = ai.Model{
		ID:        "sonnet",
		Name:      "Claude Sonnet",
		Reasoning: true,
		ToolCall:  true,
		Input:     []ai.Modality{ai.ModalityText, ai.ModalityImage},
		Output:    []ai.Modality{ai.ModalityText},
	}

	// ClaudeOpus is the most capable model.
	ClaudeOpus = ai.Model{
		ID:        "opus",
		Name:      "Claude Opus",
		Reasoning: true,
		ToolCall:  true,
		Input:     []ai.Modality{ai.ModalityText, ai.ModalityImage},
		Output:    []ai.Modality{ai.ModalityText},
	}
)

// models is the representative catalog this provider serves.
var models = []ai.Model{ClaudeSonnet, ClaudeOpus}

// Models returns the models served by the Claude CLI provider.
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
