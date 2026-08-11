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
// The final [Message] and any terminal error are not events. They are the
// [EventStream] result. Wait returns them, and the Events iterator also
// reports them.
//
// Events are JSON-serializable, so an application can forward a run to a
// client as it happens. Each optional field is omitted when it holds the
// zero value. An omitted field decodes back to that zero value.
type Event struct {
	Type EventType `json:"type"`
	// ContentIndex is the index of the content block that the event
	// belongs to. It applies to start, delta, and end events.
	ContentIndex int `json:"content_index,omitempty"`
	// Delta is incremental text for text, thinking, and tool-call deltas.
	Delta string `json:"delta,omitempty"`
	// Content is completed text (for end events).
	Content string `json:"content,omitempty"`
	// ToolCall is the completed tool call (for toolcall_end).
	ToolCall *ToolCall `json:"tool_call,omitempty"`
}
