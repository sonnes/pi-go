package ai

// StopReason indicates why generation stopped.
type StopReason string

const (
	StopReasonStop    StopReason = "stop"
	StopReasonLength  StopReason = "length"
	StopReasonToolUse StopReason = "tool_use"
	StopReasonError   StopReason = "error"
	StopReasonAborted StopReason = "aborted"
)

// EventType categorizes streaming events.
type EventType string

const (
	EventStart      EventType = "start"
	EventTextStart  EventType = "text_start"
	EventTextDelta  EventType = "text_delta"
	EventTextEnd    EventType = "text_end"
	EventThinkStart EventType = "thinking_start"
	EventThinkDelta EventType = "thinking_delta"
	EventThinkEnd   EventType = "thinking_end"
	EventToolStart  EventType = "tool_start"
	EventToolDelta  EventType = "tool_delta"
	EventToolEnd    EventType = "tool_end"
)

// Event represents a single streaming event from a model response.
// The final [Message] and any terminal error are not events — they are
// the [EventStream] result, returned by Wait and surfaced on the
// Events iterator.
//
// Events are JSON-serializable so an application can forward a run to a
// client as it happens; the zero value of every optional field is
// omitted, and omitted fields decode back to that zero value.
type Event struct {
	Type EventType `json:"type"`
	// ContentIndex is which content block the event belongs to (for
	// start/delta/end events).
	ContentIndex int `json:"content_index,omitempty"`
	// Delta is incremental text (text/thinking/toolcall deltas).
	Delta string `json:"delta,omitempty"`
	// Content is completed text (for end events).
	Content string `json:"content,omitempty"`
	// ToolCall is the completed tool call (for toolcall_end).
	ToolCall *ToolCall `json:"tool_call,omitempty"`
}
