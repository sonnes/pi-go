package agent

import (
	"encoding/json"

	"github.com/sonnes/pi-go/pkg/ai"
)

// EventType categorizes agent streaming events.
type EventType string

const (
	// EventSessionInit signals the agent backend (subprocess, in-process
	// loop, …) has been initialized. Fires exactly once per agent
	// lifetime — before the first [EventAgentStart]. For the Claude CLI
	// agent it carries the [Event.SessionID] from the subprocess
	// `system/init` line. If the backend fails to initialize at all
	// (e.g. subprocess startup error), this event is not emitted.
	EventSessionInit EventType = "session_init"

	// EventSessionEnd signals the agent backend has shut down. Fires
	// exactly once per agent lifetime, after the final [EventAgentEnd]
	// and immediately before the broker shuts down. Only emitted if
	// [EventSessionInit] previously fired. Carries [Event.Err] when the
	// backend exited with an error.
	EventSessionEnd EventType = "session_end"

	// EventAgentStart brackets a single run — one [Agent.Send],
	// [Agent.Continue], or [Agent.SendMessages] call. Fires as the first
	// event of the run, after any [EventSessionInit]. Caller-supplied
	// input messages are not echoed back. If the backend fails before
	// the run begins (e.g. subprocess startup error), this event is
	// skipped and only [EventAgentEnd] (with Err set) is emitted.
	EventAgentStart EventType = "agent_start"

	EventAgentEnd            EventType = "agent_end"
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
	Err      error

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
	case EventSessionInit:
		return json.Marshal(struct {
			Type      EventType `json:"type"`
			SessionID string    `json:"session_id,omitempty"`
		}{
			Type:      e.Type,
			SessionID: e.SessionID,
		})

	case EventSessionEnd:
		var errStr string
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		return json.Marshal(struct {
			Type  EventType `json:"type"`
			Error string    `json:"error,omitempty"`
		}{
			Type:  e.Type,
			Error: errStr,
		})

	case EventAgentStart:
		return json.Marshal(struct {
			Type      EventType `json:"type"`
			SessionID string    `json:"session_id,omitempty"`
		}{
			Type:      e.Type,
			SessionID: e.SessionID,
		})

	case EventAgentEnd:
		var errStr string
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		return json.Marshal(struct {
			Type     EventType    `json:"type"`
			Messages []ai.Message `json:"messages,omitempty"`
			Error    string       `json:"error,omitempty"`
		}{
			Type:     e.Type,
			Messages: e.Messages,
			Error:    errStr,
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
