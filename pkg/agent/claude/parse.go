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

// rawLine is a single NDJSON line from Claude CLI stdout.
// The Type field discriminates the union. The embedded `message` and
// `usage` fields carry Anthropic-API-shaped payloads and are decoded
// through the anthropic SDK types.
type rawLine struct {
	Type      string           `json:"type"`
	Subtype   string           `json:"subtype,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
	Message   json.RawMessage  `json:"message,omitempty"`
	Event     json.RawMessage  `json:"event,omitempty"`
	Result    string           `json:"result,omitempty"`
	IsError   bool             `json:"is_error,omitempty"`
	CostUSD   float64          `json:"cost_usd,omitempty"`
	Usage     *anthropic.Usage `json:"usage,omitempty"`
}

// userMessage is the wire format for type:"user" lines, which carry
// tool_result content blocks. The SDK's ToolResultBlockParam is a
// marshal-side param type and doesn't cleanly round-trip the
// string-or-array content field, so we keep a minimal hand-rolled
// decoder here.
type userMessage struct {
	Content []userContent `json:"content"`
}

// userContent is a content block inside a user NDJSON line.
// The Content field can be a string or an array of {type, text} objects
// depending on the tool; we use json.RawMessage and extract text from both.
type userContent struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// textContent extracts the text from a userContent.Content field,
// handling both string and array-of-{type,text} formats.
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

// parseLine deserializes a single NDJSON line.
func parseLine(data []byte) (rawLine, error) {
	var line rawLine
	err := json.Unmarshal(data, &line)
	return line, err
}

// toAIMessage converts an Anthropic API message (as decoded by the
// anthropic SDK) to an [ai.Message].
func toAIMessage(msg anthropic.Message) ai.Message {
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

	m.Usage = usageFromAnthropic(&msg.Usage)

	return m
}

// usageFromAnthropic converts an [anthropic.Usage] into [ai.Usage]. Pass
// nil to signal "no usage reported" (returns the zero value).
func usageFromAnthropic(u *anthropic.Usage) ai.Usage {
	if u == nil {
		return ai.Usage{}
	}
	in := int(u.InputTokens)
	out := int(u.OutputTokens)
	return ai.Usage{
		Input:      in,
		Output:     out,
		CacheRead:  int(u.CacheReadInputTokens),
		CacheWrite: int(u.CacheCreationInputTokens),
		Total:      in + out,
	}
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

// parser is a stateful converter from NDJSON lines to [agent.Event] values.
// It tracks whether a turn is open so that tool results land inside the
// same turn as the assistant's tool call, matching the Default agent's
// event protocol.
type parser struct {
	usage       ai.Usage
	messages    []ai.Message
	toolResults []ai.Message
	err         error
	inTurn      bool        // true when a turn is open (TurnStart emitted, TurnEnd not yet)
	turnMsg     *ai.Message // the assistant message for the current open turn
}

// handleLine processes a single NDJSON line and returns zero or more
// agent events. The caller pushes each returned event.
func (m *parser) handleLine(line rawLine) []agent.Event {
	switch line.Type {
	case "system":
		if line.Subtype == "init" {
			return []agent.Event{{
				Type:      agent.EventAgentStart,
				SessionID: line.SessionID,
			}}
		}
		return nil

	case "assistant":
		return m.handleAssistant(line)

	case "user":
		return m.handleUser(line)

	case "result":
		return m.handleResult(line)

	default:
		return nil
	}
}

// closeTurn emits a TurnEnd event for the current open turn, attaching
// any accumulated tool results. Resets turn state.
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
// line is a complete message (text + any tool_use blocks). Emits
// [agent.EventToolExecutionStart] for each tool call observed.
//
// If the assistant calls tools, the turn is left open so that
// subsequent tool results (type:"user") land inside the same turn.
func (m *parser) handleAssistant(line rawLine) []agent.Event {
	var msg anthropic.Message
	if err := json.Unmarshal(line.Message, &msg); err != nil {
		return nil
	}

	aiMsg := toAIMessage(msg)
	m.messages = append(m.messages, aiMsg)

	var events []agent.Event

	// Close any prior open turn before starting a new one.
	if m.inTurn {
		events = append(events, m.closeTurn())
	}

	events = append(events,
		agent.Event{Type: agent.EventTurnStart},
		agent.Event{Type: agent.EventMessageStart, Message: &aiMsg},
		agent.Event{Type: agent.EventMessageEnd, Message: &aiMsg},
	)

	toolCalls := aiMsg.ToolCalls()

	// Emit tool execution start for each tool call.
	for _, tc := range toolCalls {
		events = append(events, agent.Event{
			Type:       agent.EventToolExecutionStart,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Args:       tc.Arguments,
		})
	}

	if len(toolCalls) > 0 {
		// Keep the turn open — tool results will arrive in a user line.
		m.inTurn = true
		m.turnMsg = &aiMsg
	} else {
		// No tools — close the turn immediately.
		events = append(events, agent.Event{
			Type:    agent.EventTurnEnd,
			Message: &aiMsg,
		})
	}

	return events
}

// handleUser processes a user message line containing tool_result blocks.
// Each tool result emits [agent.EventToolExecutionEnd] and a tool result
// message pair.
func (m *parser) handleUser(line rawLine) []agent.Event {
	var msg userMessage
	if err := json.Unmarshal(line.Message, &msg); err != nil {
		return nil
	}

	var events []agent.Event
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

// handleResult processes a result line. It captures usage, handles
// errors, deduplicates result text, and populates the final turn_end
// with accumulated tool results.
func (m *parser) handleResult(line rawLine) []agent.Event {
	// Capture usage.
	if line.Usage != nil {
		m.usage = usageFromAnthropic(line.Usage)
	}

	// Map cost.
	if line.CostUSD > 0 {
		m.usage.Cost.Total = line.CostUSD
	}

	// Handle error results.
	if line.IsError {
		m.err = fmt.Errorf("claude: %s", line.Result)
		return nil
	}

	var events []agent.Event

	// Close any dangling open turn.
	if m.inTurn {
		events = append(events, m.closeTurn())
	}

	// Emit result text as a message if it wasn't already emitted
	// by an assistant line (deduplication).
	if line.Result != "" && !m.lastMessageHasText(line.Result) {
		msg := ai.AssistantMessage(ai.Text{Text: line.Result})
		msg.StopReason = ai.StopReasonStop
		m.messages = append(m.messages, msg)
		events = append(events,
			agent.Event{Type: agent.EventMessageStart, Message: &msg},
			agent.Event{Type: agent.EventMessageEnd, Message: &msg},
		)
	}

	return events
}

// lastMessageHasText reports whether the last assistant message already
// contains the given text. Used to avoid duplicating result text.
func (m *parser) lastMessageHasText(text string) bool {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role != ai.RoleAssistant {
			continue
		}
		return m.messages[i].Text() == text
	}
	return false
}
