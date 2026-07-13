package agent

import (
	"encoding/json"

	"github.com/sonnes/pi-go/pkg/ai"
)

// EventType categorizes agent streaming events.
type EventType string

const (
	// EventAgentStart brackets a single run — one [Agent.Run] call.
	// Fires as the first event of the run's [Stream]. Caller-supplied
	// input messages are not echoed back. For CLI-backed agents it
	// carries the backend's [Event.SessionID] once known.
	EventAgentStart EventType = "agent_start"

	// EventAgentEnd is the final event of a successful run, carrying
	// the new [Event.Messages] and accumulated [Event.Usage]. When the
	// run fails there is no agent_end — the error ends the [Stream]
	// iteration instead.
	EventAgentEnd EventType = "agent_end"

	EventTurnStart           EventType = "turn_start"
	EventTurnEnd             EventType = "turn_end"
	EventMessageStart        EventType = "message_start"
	EventMessageUpdate       EventType = "message_update"
	EventMessageEnd          EventType = "message_end"
	EventToolExecutionStart  EventType = "tool_execution_start"
	EventToolExecutionUpdate EventType = "tool_execution_update"
	EventToolExecutionEnd    EventType = "tool_execution_end"
)

// Event represents a single agent streaming event.
// Fields are populated based on Type — unused fields are zero-valued.
type Event struct {
	Type EventType

	// agent_start
	SessionID string // session identifier from the subprocess

	// agent_end
	Messages []ai.Message // all new messages produced this run
	Usage    ai.Usage     // accumulated usage across all turns

	// turn_end
	ToolResults []ai.Message // tool result messages from this turn

	// message_start, message_update, message_end, turn_end
	Message *ai.Message

	// Input is true on message_start/message_end events emitted for
	// messages a [BeforeStop] hook injected to keep the loop going.
	// Caller-supplied input messages (from [Agent.Send] /
	// [Agent.SendMessages]) are not echoed at all — the caller has
	// already stored them. Consumers persisting from the event stream
	// can ignore this field unless they also use BeforeStop hooks.
	Input bool

	// message_update
	AssistantEvent *ai.Event // underlying ai event

	// tool_execution_start, tool_execution_update, tool_execution_end
	ToolCallID string
	ToolName   string
	Args       map[string]any // tool_execution_start

	// tool_execution_update
	PartialResult any

	// tool_execution_end
	Result  any
	IsError bool
}

// MarshalJSON encodes Event to JSON with only the relevant fields for each
// event type, keeping the wire format clean.
func (e Event) MarshalJSON() ([]byte, error) {
	switch e.Type {
	case EventAgentStart:
		return json.Marshal(struct {
			Type      EventType `json:"type"`
			SessionID string    `json:"session_id,omitempty"`
		}{
			Type:      e.Type,
			SessionID: e.SessionID,
		})

	case EventAgentEnd:
		return json.Marshal(struct {
			Type     EventType    `json:"type"`
			Messages []ai.Message `json:"messages,omitempty"`
		}{
			Type:     e.Type,
			Messages: e.Messages,
		})

	case EventTurnStart:
		return json.Marshal(struct {
			Type EventType `json:"type"`
		}{
			Type: e.Type,
		})

	case EventTurnEnd:
		return json.Marshal(struct {
			Type        EventType    `json:"type"`
			Message     *ai.Message  `json:"message,omitempty"`
			ToolResults []ai.Message `json:"tool_results,omitempty"`
		}{
			Type:        e.Type,
			Message:     e.Message,
			ToolResults: e.ToolResults,
		})

	case EventMessageStart, EventMessageEnd:
		return json.Marshal(struct {
			Type    EventType   `json:"type"`
			Message *ai.Message `json:"message,omitempty"`
		}{
			Type:    e.Type,
			Message: e.Message,
		})

	case EventMessageUpdate:
		return json.Marshal(struct {
			Type           EventType   `json:"type"`
			Message        *ai.Message `json:"message,omitempty"`
			AssistantEvent *ai.Event   `json:"assistant_event,omitempty"`
		}{
			Type:           e.Type,
			Message:        e.Message,
			AssistantEvent: e.AssistantEvent,
		})

	case EventToolExecutionStart:
		return json.Marshal(struct {
			Type       EventType      `json:"type"`
			ToolCallID string         `json:"tool_call_id"`
			ToolName   string         `json:"tool_name"`
			Args       map[string]any `json:"args"`
		}{
			Type:       e.Type,
			ToolCallID: e.ToolCallID,
			ToolName:   e.ToolName,
			Args:       e.Args,
		})

	case EventToolExecutionUpdate:
		return json.Marshal(struct {
			Type          EventType `json:"type"`
			ToolCallID    string    `json:"tool_call_id"`
			ToolName      string    `json:"tool_name"`
			PartialResult any       `json:"partial_result,omitempty"`
		}{
			Type:          e.Type,
			ToolCallID:    e.ToolCallID,
			ToolName:      e.ToolName,
			PartialResult: e.PartialResult,
		})

	case EventToolExecutionEnd:
		return json.Marshal(struct {
			Type       EventType `json:"type"`
			ToolCallID string    `json:"tool_call_id"`
			ToolName   string    `json:"tool_name"`
			Result     any       `json:"result,omitempty"`
			IsError    bool      `json:"is_error"`
		}{
			Type:       e.Type,
			ToolCallID: e.ToolCallID,
			ToolName:   e.ToolName,
			Result:     e.Result,
			IsError:    e.IsError,
		})

	default:
		return json.Marshal(struct {
			Type EventType `json:"type"`
		}{
			Type: e.Type,
		})
	}
}
