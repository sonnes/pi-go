package agent

import "github.com/sonnes/pi-go/pkg/ai"

// Role categorizes agent messages.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "tool_result"
	RoleSystem     Role = "system"
)

// Message is any message in an agent conversation.
// It is either an [LLMMessage] wrapping [ai.Message],
// or a custom application message embedding [CustomMessage].
//
// The interface is sealed — external packages extend it by
// embedding [CustomMessage], not by implementing the marker directly.
type Message interface {
	agentMessage() // sealed marker
	Role() Role
}

// LLMMessage wraps an [ai.Message] as an agent [Message].
type LLMMessage struct {
	ai.Message
}

func (LLMMessage) agentMessage() {}

// Role returns the agent [Role] corresponding to the underlying [ai.Role].
func (m LLMMessage) Role() Role { return Role(m.Message.Role) }

// NewLLMMessage wraps an [ai.Message] into an [LLMMessage].
func NewLLMMessage(m ai.Message) LLMMessage {
	return LLMMessage{Message: m}
}

// CustomMessage is a base type that application-defined messages
// embed to satisfy the [Message] interface.
//
//	type ArtifactMessage struct {
//	    agent.CustomMessage
//	    Title   string
//	    Content string
//	}
type CustomMessage struct {
	CustomRole Role
	Kind       string
}

func (CustomMessage) agentMessage() {}

// Role returns the [CustomRole] assigned to this message.
func (m CustomMessage) Role() Role { return m.CustomRole }

// AsLLMMessage extracts the [LLMMessage] from a [Message], if it is one.
func AsLLMMessage(m Message) (LLMMessage, bool) {
	lm, ok := m.(LLMMessage)
	return lm, ok
}

// LLMMessages filters a slice of [Message] down to the underlying
// [ai.Message] values, dropping any custom messages.
func LLMMessages(msgs []Message) []ai.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]ai.Message, 0, len(msgs))
	for _, m := range msgs {
		if lm, ok := m.(LLMMessage); ok {
			out = append(out, lm.Message)
		}
	}
	return out
}
