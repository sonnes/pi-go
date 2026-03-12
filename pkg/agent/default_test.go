package agent

import (
	"context"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
)

func TestNewDefault_Defaults(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	a := New(model)

	s := a.State()
	assert.False(t, s.IsStreaming())
	assert.Nil(t, s.StreamMessage())
	assert.Empty(t, s.PendingToolCalls())
	assert.NoError(t, s.Err())
	assert.Empty(t, s.Messages())
	assert.Equal(t, model, a.config.model)
	assert.Empty(t, a.config.tools)
	assert.Equal(t, 0, a.config.maxTurns)
}

func TestNewDefault_WithTools(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	tool := ai.DefineTool[string, string](
		"echo",
		"echoes input",
		func(_ context.Context, in string) (string, error) {
			return in, nil
		},
	)

	a := New(model, WithTools(tool))

	assert.Len(t, a.config.tools, 1)
}

func TestNewDefault_WithHistory(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	msgs := []Message{
		NewLLMMessage(ai.UserMessage("hello")),
		NewLLMMessage(ai.AssistantMessage(ai.Text{Text: "hi"})),
	}

	a := New(model, WithHistory(msgs...))

	s := a.State()
	assert.Equal(t, msgs, s.Messages())
}

func TestNewDefault_WithHistory_IsCopied(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	msgs := []Message{NewLLMMessage(ai.UserMessage("hello"))}

	a := New(model, WithHistory(msgs...))

	// Mutate original — should not affect agent state.
	msgs[0] = NewLLMMessage(ai.UserMessage("modified"))

	got := a.State().Messages()
	lm, ok := AsLLMMessage(got[0])
	assert.True(t, ok)
	assert.Equal(t, "hello", lm.Content[0].(ai.Text).Text)
}

func TestNewDefault_WithMaxTurns(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	a := New(model, WithMaxTurns(5))

	assert.Equal(t, 5, a.config.maxTurns)
}

func TestNewDefault_WithSystemPrompt(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	prompt := Prompt{Sections: []PromptSection{}}

	a := New(model, WithSystemPrompt(prompt))

	assert.Equal(t, prompt, a.config.systemPrompt)
}

func TestNewDefault_WithStreamOpts(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	opts := []ai.Option{ai.WithMaxTokens(100)}

	a := New(model, WithStreamOpts(opts...))

	assert.Len(t, a.config.streamOpts, 1)
}

func TestNewDefault_MultipleOptions(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	tool := ai.DefineTool[string, string](
		"echo",
		"echoes input",
		func(_ context.Context, in string) (string, error) {
			return in, nil
		},
	)
	msgs := []Message{NewLLMMessage(ai.UserMessage("hello"))}

	a := New(
		model,
		WithTools(tool),
		WithHistory(msgs...),
		WithMaxTurns(10),
	)

	assert.Len(t, a.config.tools, 1)
	assert.Len(t, a.State().Messages(), 1)
	assert.Equal(t, 10, a.config.maxTurns)
}
