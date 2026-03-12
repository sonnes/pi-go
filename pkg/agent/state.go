package agent

import (
	"maps"

	"github.com/sonnes/pi-go/pkg/ai"
)

// State is a point-in-time snapshot of the agent's runtime state.
// All fields are unexported; use getter methods to read values.
// State is safe to pass across goroutines — getters return copies
// of mutable data.
type State struct {
	isStreaming      bool
	streamMessage    *ai.Message
	pendingToolCalls map[string]struct{}
	err              error
	messages         []Message
}

// IsStreaming reports whether the agent loop is currently executing.
func (s State) IsStreaming() bool {
	return s.isStreaming
}

// StreamMessage returns the partial assistant message being streamed,
// or nil if not currently streaming a message.
func (s State) StreamMessage() *ai.Message {
	return s.streamMessage
}

// PendingToolCalls returns a copy of the set of in-flight tool call IDs.
func (s State) PendingToolCalls() map[string]struct{} {
	if len(s.pendingToolCalls) == 0 {
		return nil
	}
	return maps.Clone(s.pendingToolCalls)
}

// Err returns the last error encountered during the agent loop, or nil.
func (s State) Err() error {
	return s.err
}

// Messages returns a copy of the current conversation history.
func (s State) Messages() []Message {
	if len(s.messages) == 0 {
		return nil
	}
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// LLMMessages returns only the [ai.Message] values from the
// conversation history, filtering out custom messages.
func (s State) LLMMessages() []ai.Message {
	return LLMMessages(s.messages)
}
