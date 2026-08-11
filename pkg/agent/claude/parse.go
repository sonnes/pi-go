package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// --- NDJSON wire types ---

// rawLine is one NDJSON line from Claude CLI stdout. The Type field
// selects the member of the union. The embedded `message` and `usage`
// fields carry payloads in the shape of the Anthropic API. The anthropic
// SDK types decode them.
type rawLine struct {
	Type      string           `json:"type"`
	Subtype   string           `json:"subtype,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
	Message   json.RawMessage  `json:"message,omitempty"`
	Event     json.RawMessage  `json:"event,omitempty"`
	Result    string           `json:"result,omitempty"`
	IsError   bool             `json:"is_error,omitempty"`
	Usage     *anthropic.Usage `json:"usage,omitempty"`

	// control_request lines from the CLI to the client, for example
	// can_use_tool.
	RequestID string          `json:"request_id,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
}

// userMessage is the wire format for type:"user" lines. These lines
// carry tool_result content blocks. The ToolResultBlockParam type of the
// SDK is a marshal-side param type. It does not round-trip the content
// field, which is a string or an array. This package therefore keeps a
// small decoder of its own here.
type userMessage struct {
	Content []userContent `json:"content"`
}

// userContent is a content block inside a user NDJSON line. The Content
// field is a string or an array of {type, text} objects. The tool
// decides which one. This package uses json.RawMessage and extracts the
// text from both forms.
type userContent struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// textContent extracts the text from a userContent.Content field. It
// accepts the string format and the array-of-{type,text} format.
func (c userContent) textContent() string {
	if len(c.Content) == 0 {
		return ""
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(c.Content, &s); err == nil {
		return s
	}

	// Try array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(c.Content, &blocks); err == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}

	return string(c.Content)
}

// parseLine deserializes one NDJSON line.
func parseLine(data []byte) (rawLine, error) {
	var line rawLine
	err := json.Unmarshal(data, &line)
	return line, err
}

// toAIMessage converts an Anthropic API message to an [ai.Message]. The
// anthropic SDK decodes the input message.
func toAIMessage(model ai.Model, msg anthropic.Message) ai.Message {
	m := ai.Message{
		Role:       ai.RoleAssistant,
		StopReason: mapStopReason(string(msg.StopReason)),
	}

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			m.Content = append(m.Content, ai.Text{Text: block.Text})
		case "thinking":
			m.Content = append(m.Content, ai.Thinking{
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "tool_use":
			var args map[string]any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &args)
			}
			m.Content = append(m.Content, ai.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}

	m.Usage = usageFromAnthropic(model, &msg.Usage)

	return m
}

// usageFromAnthropic converts an [anthropic.Usage] into [ai.Usage]. To
// signal that there is no reported usage, pass nil. The function then
// returns the zero value.
func usageFromAnthropic(model ai.Model, u *anthropic.Usage) ai.Usage {
	if u == nil {
		return ai.Usage{}
	}
	usage := ai.Usage{
		Input:      int(u.InputTokens),
		Output:     int(u.OutputTokens),
		CacheRead:  int(u.CacheReadInputTokens),
		CacheWrite: int(u.CacheCreationInputTokens),
	}

	rates := model.Cost
	usage.Cost = ai.UsageCost{
		Input:      perMillion(usage.Input, rates.Input),
		Output:     perMillion(usage.Output, rates.Output),
		CacheRead:  perMillion(usage.CacheRead, rates.CacheRead),
		CacheWrite: perMillion(usage.CacheWrite, rates.CacheWrite),
	}

	return usage
}

// mapStopReason converts Anthropic API stop reasons to [ai.StopReason].
func mapStopReason(reason string) ai.StopReason {
	switch reason {
	case "tool_use":
		return ai.StopReasonToolUse
	case "max_tokens":
		return ai.StopReasonLength
	default:
		return ai.StopReasonStop
	}
}

// --- event parser ---

// parser is a stateful converter from NDJSON lines to [agent.Event]
// values. It records whether a turn is open. As a result, the tool
// results stay in the same turn as the tool call of the assistant. This
// matches the event protocol of the Default agent.
type parser struct {
	// model is the caller's [ai.Model]. The parser keeps it for the Cost
	// rates. The CLI reports token counts but no cost per category.
	model       ai.Model
	usage       ai.Usage
	messages    []ai.Message
	toolResults []ai.Message
	err         error
	inTurn      bool        // true after TurnStart and before TurnEnd
	turnMsg     *ai.Message // the assistant message for the current open turn

	// The usage on an assistant line is a streaming snapshot. It usually
	// holds input_tokens only, with output_tokens=0. The final usage
	// arrives on the last result line. The parser therefore buffers
	// MessageEnd and TurnEnd for a stop-reason assistant here, until
	// [handleResult] attaches the final usage through [lastAssistantMsg].
	pending          []agent.Event
	lastAssistantMsg *ai.Message

	// streamOpen is true after a stream_event content_block_start emits
	// the turn_start/message_start bracket for the message in progress.
	// The assistant line that follows must not emit the bracket again.
	// That assistant line clears the field. The result line also clears
	// it.
	streamOpen bool
}

// handleLine processes one NDJSON line and returns zero or more agent
// events. The caller pushes each returned event.
//
// [Agent.readLoop] intercepts the system/init lines, not the parser. The
// agent records the session ID from init. readLoop publishes the per-run
// [agent.EventAgentStart] immediately before the next batch of parser
// events. [Agent.expectAgentStart] signals that publish. On success,
// [Agent.awaitTurn] emits [agent.EventAgentEnd].
func (m *parser) handleLine(line rawLine) []agent.Event {
	switch line.Type {
	case "assistant":
		return m.handleAssistant(line)

	case "user":
		return m.handleUser(line)

	case "result":
		return m.handleResult(line)

	case "stream_event":
		return m.handleStreamEvent(line)

	default:
		return nil
	}
}

// --- stream_event handling (--include-partial-messages) ---

// streamEvent is the Anthropic SSE event inside a stream_event line.
// Only content_block_start and content_block_delta produce agent events.
// The parser ignores message_start, message_delta, message_stop,
// content_block_stop, and ping.
type streamEvent struct {
	Type  string       `json:"type"`
	Index int          `json:"index"`
	Delta *streamDelta `json:"delta,omitempty"`
}

// streamDelta is a content_block_delta payload. The Type field selects
// which value field holds the data.
type streamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

// handleStreamEvent processes a stream_event line. A content_block_start
// opens the message bracket early with an empty message. The deltas that
// follow then stream as [agent.EventMessageUpdate] between message_start
// and message_end. This matches the lifecycle of the Default agent. The
// assistant line that completes each block carries the final content.
// That line closes the bracket through [handleAssistant].
func (m *parser) handleStreamEvent(line rawLine) []agent.Event {
	var ev streamEvent
	if err := json.Unmarshal(line.Event, &ev); err != nil {
		return nil
	}

	switch ev.Type {
	case "content_block_start":
		return m.openStreamMessage()

	case "content_block_delta":
		if !m.streamOpen || ev.Delta == nil {
			return nil
		}
		aiEvt := mapStreamDelta(ev.Index, *ev.Delta)
		if aiEvt == nil {
			return nil
		}
		return []agent.Event{{
			Type:           agent.EventMessageUpdate,
			AssistantEvent: aiEvt,
		}}

	default:
		return nil
	}
}

// openStreamMessage emits the turn_start/message_start bracket for a
// streamed message. The CLI emits one assistant line for each completed
// content block. Each content_block_start after an assistant line
// therefore starts a new message. Before the new message, the parser
// flushes any buffered end events and closes any open tool-use turn.
// [handleAssistant] applies the same boundary on the path without
// streaming.
func (m *parser) openStreamMessage() []agent.Event {
	if m.streamOpen {
		return nil
	}

	var events []agent.Event
	events = append(events, m.flushPending()...)
	if m.inTurn {
		events = append(events, m.closeTurn())
	}

	m.streamOpen = true
	empty := ai.Message{Role: ai.RoleAssistant}
	events = append(events,
		agent.Event{Type: agent.EventTurnStart},
		agent.Event{Type: agent.EventMessageStart, Message: &empty},
	)
	return events
}

// mapStreamDelta converts a content_block_delta into the [ai.Event] that
// message_update carries. For signature_delta and for unknown delta
// types, the function returns nil, because they carry nothing to show.
func mapStreamDelta(index int, d streamDelta) *ai.Event {
	switch d.Type {
	case "text_delta":
		return &ai.Event{
			Type:         ai.EventTextDelta,
			ContentIndex: index,
			Delta:        d.Text,
		}
	case "thinking_delta":
		return &ai.Event{
			Type:         ai.EventThinkDelta,
			ContentIndex: index,
			Delta:        d.Thinking,
		}
	case "input_json_delta":
		return &ai.Event{
			Type:         ai.EventToolDelta,
			ContentIndex: index,
			Delta:        d.PartialJSON,
		}
	default:
		return nil
	}
}

// closeTurn emits a TurnEnd event for the current open turn. It attaches
// any accumulated tool results. It then resets the turn state.
func (m *parser) closeTurn() agent.Event {
	evt := agent.Event{
		Type:    agent.EventTurnEnd,
		Message: m.turnMsg,
	}
	if len(m.toolResults) > 0 {
		evt.ToolResults = m.toolResults
		m.toolResults = nil
	}
	m.inTurn = false
	m.turnMsg = nil
	return evt
}

// handleAssistant processes an assistant message line. Each assistant
// line is a complete message with text and any tool_use blocks. The
// function emits [agent.EventToolExecutionStart] for each tool call that
// it sees.
//
// If the assistant calls tools, the turn stays open. The tool results
// that follow, on type:"user" lines, then land inside the same turn.
//
// For a stop-reason message, which has no tool calls, the parser buffers
// MessageEnd and TurnEnd. [handleResult] flushes them. The final usage
// from the result line can then attach to the message.
func (m *parser) handleAssistant(line rawLine) []agent.Event {
	var msg anthropic.Message
	if err := json.Unmarshal(line.Message, &msg); err != nil {
		return nil
	}

	aiMsg := toAIMessage(m.model, msg)
	// The per-line usage from the CLI is a streaming snapshot.
	// output_tokens is usually 0 here. Remove it. The result line carries
	// the final usage for the whole turn.
	aiMsg.Usage = ai.Usage{}

	var events []agent.Event

	// If the stream events already opened the bracket of this message,
	// content_block_start emitted the boundary. The boundary is the
	// pending flush, the turn close, turn_start, and message_start. Do
	// not repeat it here.
	streamed := m.streamOpen
	m.streamOpen = false

	if !streamed {
		// A new assistant line means that the earlier pending buffer does
		// not receive the usage from the result line. Flush the buffer now
		// and attach no usage.
		events = append(events, m.flushPending()...)

		// Close any prior open turn before starting a new one.
		if m.inTurn {
			events = append(events, m.closeTurn())
		}
	}

	m.messages = append(m.messages, aiMsg)

	if !streamed {
		events = append(events,
			agent.Event{Type: agent.EventTurnStart},
			agent.Event{Type: agent.EventMessageStart, Message: &aiMsg},
		)
	}

	toolCalls := aiMsg.ToolCalls()

	if len(toolCalls) > 0 {
		// A tool-use assistant finishes at once. The turn stays open and
		// waits for the tool results. The turn-level usage belongs to the
		// final stop-reason message, not to this one. No buffer is
		// necessary.
		events = append(events, agent.Event{
			Type:    agent.EventMessageEnd,
			Message: &aiMsg,
		})
		for _, tc := range toolCalls {
			events = append(events, agent.Event{
				Type:       agent.EventToolExecutionStart,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Args:       tc.Arguments,
			})
		}
		m.inTurn = true
		m.turnMsg = &aiMsg
	} else {
		// This is a stop-reason assistant. Buffer MessageEnd and TurnEnd
		// until [handleResult] attaches the final usage to aiMsg.
		m.pending = []agent.Event{
			{Type: agent.EventMessageEnd, Message: &aiMsg},
			{Type: agent.EventTurnEnd, Message: &aiMsg},
		}
		m.lastAssistantMsg = &aiMsg
	}

	return events
}

// flushPending returns any buffered end-of-turn events and clears the
// buffer. If the result line drives the flush, the caller must attach
// the final usage to [lastAssistantMsg] first.
func (m *parser) flushPending() []agent.Event {
	if len(m.pending) == 0 {
		return nil
	}
	out := m.pending
	m.pending = nil
	m.lastAssistantMsg = nil
	return out
}

// handleUser processes a user message line with tool_result blocks. Each
// tool result emits [agent.EventToolExecutionEnd] and a pair of tool
// result messages.
func (m *parser) handleUser(line rawLine) []agent.Event {
	var msg userMessage
	if err := json.Unmarshal(line.Message, &msg); err != nil {
		return nil
	}

	// Tool results do not normally follow a stop-reason assistant line.
	// If they do, flush the pending buffer first.
	events := m.flushPending()
	for _, block := range msg.Content {
		if block.Type != "tool_result" {
			continue
		}

		text := block.textContent()
		resultMsg := ai.ToolResultMessage(
			block.ToolUseID,
			"",
			ai.Text{Text: text},
		)
		if block.IsError {
			resultMsg.IsError = true
		}
		m.messages = append(m.messages, resultMsg)
		m.toolResults = append(m.toolResults, resultMsg)

		events = append(events,
			agent.Event{
				Type:       agent.EventToolExecutionEnd,
				ToolCallID: block.ToolUseID,
				Result:     text,
				IsError:    block.IsError,
			},
			agent.Event{
				Type:    agent.EventMessageStart,
				Message: &resultMsg,
			},
			agent.Event{
				Type:    agent.EventMessageEnd,
				Message: &resultMsg,
			},
		)
	}

	return events
}

// handleResult processes a result line. It captures the usage, handles
// the errors, removes duplicate result text, and fills the final
// turn_end with the accumulated tool results.
//
// The final usage from the result line attaches to the last message of
// the turn. In the common case, that message is the buffered stop-reason
// assistant message. If the assistant emitted only thinking,
// handleResult builds a new message from line.Result instead.
func (m *parser) handleResult(line rawLine) []agent.Event {
	// An interrupted turn can leave a streamed message bracket open, with
	// no assistant line to close it. The result line ends the turn in
	// both cases.
	m.streamOpen = false

	// Capture usage.
	if line.Usage != nil {
		m.usage = usageFromAnthropic(m.model, line.Usage)
	}

	// Handle error results.
	if line.IsError {
		m.err = fmt.Errorf("claude: %s", line.Result)
		return nil
	}

	var events []agent.Event

	// Close any open turn that still dangles. This is the tool-use path
	// with no stop-reason assistant after it.
	if m.inTurn {
		events = append(events, m.closeTurn())
	}

	// Decide where the final usage lands. If the assistant emitted no
	// matching text, it lands on a new message. If not, it lands on the
	// buffered stop-reason assistant message.
	synthesizing := line.Result != "" && !m.lastMessageHasText(line.Result)

	if synthesizing {
		// The buffered message, if there is one, is not the final
		// message. Flush it and attach no usage.
		events = append(events, m.flushPending()...)

		msg := ai.AssistantMessage(ai.Text{Text: line.Result})
		msg.StopReason = ai.StopReasonStop
		msg.Usage = m.usage
		m.messages = append(m.messages, msg)
		events = append(events,
			agent.Event{Type: agent.EventMessageStart, Message: &msg},
			agent.Event{Type: agent.EventMessageEnd, Message: &msg},
		)
		return events
	}

	// There is no new message. Attach the final usage to the last
	// assistant message that the parser emitted. The shared pointer
	// carries it to the buffered MessageEnd. Attach it also to the stored
	// copy in m.messages, which EventAgentEnd carries.
	if m.lastAssistantMsg != nil {
		m.lastAssistantMsg.Usage = m.usage
	}
	if n := len(m.messages); n > 0 && m.messages[n-1].Role == ai.RoleAssistant {
		m.messages[n-1].Usage = m.usage
	}
	events = append(events, m.flushPending()...)

	return events
}

// lastMessageHasText reports whether the last assistant message already
// contains the given text. The parser uses it to prevent duplicate
// result text.
func (m *parser) lastMessageHasText(text string) bool {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role != ai.RoleAssistant {
			continue
		}
		return m.messages[i].Text() == text
	}
	return false
}

// perMillion prices tokens at rate, which is quoted per million tokens.
func perMillion(tokens int, rate float64) float64 {
	return float64(tokens) * rate / 1_000_000
}
