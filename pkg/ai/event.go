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
type Event struct {
	Type         EventType
	ContentIndex int       // which content block (for start/delta/end events)
	Delta        string    // incremental text (text/thinking/toolcall deltas)
	Content      string    // completed text (for end events)
	ToolCall     *ToolCall // completed tool call (for toolcall_end)
}
