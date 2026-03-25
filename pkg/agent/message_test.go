package agent

import (
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// artifactMessage is a test custom message type.
type artifactMessage struct {
	CustomMessage
	Title   string
	Content string
}

func TestLLMMessage_ImplementsMessage(t *testing.T) {
	var _ Message = LLMMessage{}
}

func TestCustomMessage_ImplementsMessage(t *testing.T) {
	var _ Message = CustomMessage{}
}

func TestCustomMessage_EmbeddedImplementsMessage(t *testing.T) {
	var _ Message = artifactMessage{}
}

func TestLLMMessage_Role(t *testing.T) {
	tests := []struct {
		name string
		msg  ai.Message
		want Role
	}{
		{
			name: "user",
			msg:  ai.UserMessage("hello"),
			want: RoleUser,
		},
		{
			name: "assistant",
			msg:  ai.AssistantMessage(ai.Text{Text: "hi"}),
			want: RoleAssistant,
		},
		{
			name: "tool_result",
			msg:  ai.ToolResultMessage("id", "name", ai.Text{Text: "ok"}),
			want: RoleToolResult,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := LLMMessage{Message: tt.msg}
			assert.Equal(t, tt.want, m.Role())
		})
	}
}

func TestCustomMessage_Role(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		m := CustomMessage{Kind: "status"}
		assert.Equal(t, Role(""), m.Role())
	})

	t.Run("explicit role", func(t *testing.T) {
		m := CustomMessage{CustomRole: RoleSystem, Kind: "system"}
		assert.Equal(t, RoleSystem, m.Role())
	})
}

func TestCustomMessage_Embedded_Role(t *testing.T) {
	m := artifactMessage{
		CustomMessage: CustomMessage{CustomRole: RoleUser, Kind: "artifact"},
	}
	assert.Equal(t, RoleUser, m.Role())
}

func TestRole_ViaInterface(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want Role
	}{
		{
			name: "llm user",
			msg:  NewLLMMessage(ai.UserMessage("hi")),
			want: RoleUser,
		},
		{
			name: "custom with system role",
			msg:  CustomMessage{CustomRole: RoleSystem, Kind: "instruction"},
			want: RoleSystem,
		},
		{
			name: "custom with explicit role",
			msg:  CustomMessage{CustomRole: RoleUser, Kind: "artifact"},
			want: RoleUser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.msg.Role())
		})
	}
}

func TestCustomMessage_Kind(t *testing.T) {
	m := CustomMessage{Kind: "artifact"}
	assert.Equal(t, "artifact", m.Kind)
}

func TestCustomMessage_Embedded_Kind(t *testing.T) {
	m := artifactMessage{
		CustomMessage: CustomMessage{Kind: "artifact"},
		Title:         "code",
		Content:       "func main() {}",
	}
	assert.Equal(t, "artifact", m.Kind)
}

func TestAsLLMMessage(t *testing.T) {
	t.Run("from LLMMessage", func(t *testing.T) {
		orig := ai.UserMessage("hello")
		m := LLMMessage{Message: orig}

		got, ok := AsLLMMessage(m)
		require.True(t, ok)
		assert.Equal(t, orig, got.Message)
	})

	t.Run("from CustomMessage", func(t *testing.T) {
		m := CustomMessage{Kind: "status"}

		_, ok := AsLLMMessage(m)
		assert.False(t, ok)
	})

	t.Run("from embedded CustomMessage", func(t *testing.T) {
		m := artifactMessage{
			CustomMessage: CustomMessage{Kind: "artifact"},
		}

		_, ok := AsLLMMessage(m)
		assert.False(t, ok)
	})
}

func TestLLMMessages(t *testing.T) {
	t.Run("filters custom messages", func(t *testing.T) {
		msgs := []Message{
			LLMMessage{Message: ai.UserMessage("hello")},
			CustomMessage{Kind: "status"},
			LLMMessage{Message: ai.AssistantMessage(ai.Text{Text: "hi"})},
			artifactMessage{
				CustomMessage: CustomMessage{Kind: "artifact"},
			},
			LLMMessage{Message: ai.ToolResultMessage("id", "name", ai.Text{Text: "ok"})},
		}

		got := LLMMessages(msgs)

		require.Len(t, got, 3)
		assert.Equal(t, ai.RoleUser, got[0].Role)
		assert.Equal(t, ai.RoleAssistant, got[1].Role)
		assert.Equal(t, ai.RoleToolResult, got[2].Role)
	})

	t.Run("empty input", func(t *testing.T) {
		got := LLMMessages(nil)
		assert.Nil(t, got)
	})

	t.Run("all custom", func(t *testing.T) {
		msgs := []Message{
			CustomMessage{Kind: "a"},
			CustomMessage{Kind: "b"},
		}

		got := LLMMessages(msgs)
		assert.Empty(t, got)
	})

	t.Run("all LLM", func(t *testing.T) {
		msgs := []Message{
			LLMMessage{Message: ai.UserMessage("one")},
			LLMMessage{Message: ai.UserMessage("two")},
		}

		got := LLMMessages(msgs)
		require.Len(t, got, 2)
	})
}

func TestNewLLMMessage(t *testing.T) {
	orig := ai.UserMessage("hello")
	m := NewLLMMessage(orig)

	assert.Equal(t, orig, m.Message)
}

func TestCustomMessage_StructLiteral(t *testing.T) {
	m := CustomMessage{CustomRole: RoleSystem, Kind: "instruction"}
	assert.Equal(t, "instruction", m.Kind)
	assert.Equal(t, RoleSystem, m.Role())
}

func TestFilterMessages(t *testing.T) {
	msgs := []Message{
		LLMMessage{Message: ai.UserMessage("hello")},
		CustomMessage{Kind: "status"},
		LLMMessage{Message: ai.AssistantMessage(ai.Text{Text: "hi"})},
		artifactMessage{
			CustomMessage: CustomMessage{Kind: "artifact"},
			Title:         "code",
		},
		LLMMessage{Message: ai.ToolResultMessage("id", "name", ai.Text{Text: "ok"})},
	}

	t.Run("filter LLMMessage", func(t *testing.T) {
		got := FilterMessages[LLMMessage](msgs)
		require.Len(t, got, 3)
		assert.Equal(t, RoleUser, got[0].Role())
		assert.Equal(t, RoleAssistant, got[1].Role())
		assert.Equal(t, RoleToolResult, got[2].Role())
	})

	t.Run("filter CustomMessage", func(t *testing.T) {
		got := FilterMessages[CustomMessage](msgs)
		require.Len(t, got, 1)
		assert.Equal(t, "status", got[0].Kind)
	})

	t.Run("filter artifactMessage", func(t *testing.T) {
		got := FilterMessages[artifactMessage](msgs)
		require.Len(t, got, 1)
		assert.Equal(t, "code", got[0].Title)
	})

	t.Run("empty input", func(t *testing.T) {
		got := FilterMessages[LLMMessage](nil)
		assert.Nil(t, got)
	})

	t.Run("no matches", func(t *testing.T) {
		got := FilterMessages[artifactMessage]([]Message{
			LLMMessage{Message: ai.UserMessage("hello")},
		})
		assert.Nil(t, got)
	})
}

func TestMessageTypeSwitch(t *testing.T) {
	msgs := []Message{
		LLMMessage{Message: ai.UserMessage("hello")},
		artifactMessage{
			CustomMessage: CustomMessage{Kind: "artifact"},
			Title:         "code",
		},
	}

	var llmCount, customCount int
	for _, m := range msgs {
		switch m.(type) {
		case LLMMessage:
			llmCount++
		case artifactMessage:
			customCount++
		}
	}

	assert.Equal(t, 1, llmCount)
	assert.Equal(t, 1, customCount)
}
