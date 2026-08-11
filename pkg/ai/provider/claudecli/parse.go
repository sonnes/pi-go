package claudecli

import (
	"encoding/json"

	"github.com/sonnes/pi-go/pkg/ai"
)

// rawLine is a single NDJSON line from Claude CLI stdout.
type rawLine struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Usage     *rawUsage       `json:"usage,omitempty"`
}

// streamEvent is an Anthropic SSE event that Claude Code embeds when the
// --include-partial-messages flag is set.
type streamEvent struct {
	Type         string              `json:"type"`
	Index        int                 `json:"index"`
	ContentBlock *streamContentBlock `json:"content_block,omitempty"`
	Delta        *streamDelta        `json:"delta,omitempty"`
}

type streamContentBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

type streamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// anthropicMessage is the Anthropic API message format inside assistant
// event lines.
type anthropicMessage struct {
	Role       string             `json:"role"`
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      *anthropicUsage    `json:"usage,omitempty"`
}

type anthropicContent struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	Thinking string         `json:"thinking,omitempty"`
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// parseLine deserializes a single NDJSON line.
func parseLine(data []byte) (rawLine, error) {
	var line rawLine
	err := json.Unmarshal(data, &line)
	return line, err
}

// toAIMessage converts an Anthropic API message to an [ai.Message]. It
// prices the usage with the rates of model.
func toAIMessage(model ai.Model, msg anthropicMessage) ai.Message {
	m := ai.Message{
		Role:       ai.Role(msg.Role),
		StopReason: mapStopReason(msg.StopReason),
	}

	for _, c := range msg.Content {
		switch c.Type {
		case "text":
			m.Content = append(m.Content, ai.Text{Text: c.Text})
		case "thinking":
			m.Content = append(m.Content, ai.Thinking{Thinking: c.Thinking})
		case "tool_use":
			m.Content = append(m.Content, ai.ToolCall{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: c.Input,
			})
		}
	}

	if msg.Usage != nil {
		m.Usage = ai.Usage{
			Input:      msg.Usage.InputTokens,
			Output:     msg.Usage.OutputTokens,
			CacheRead:  msg.Usage.CacheReadInputTokens,
			CacheWrite: msg.Usage.CacheCreationInputTokens,
		}
		m.Usage.Cost = anthropicCost(model, m.Usage)
	}

	return m
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

// anthropicCost prices an Anthropic-shaped usage block with the rates of
// model.
//
// The CLI relays the usage block of Anthropic. In that block, input_tokens
// excludes the cached prefix. The four token kinds are therefore disjoint,
// and each kind bills once at its own rate. The rates come from the
// [ai.Model] of the caller. The CLI reports only a total_cost_usd sum with
// no split by category, so there is no other value to relay.
func anthropicCost(model ai.Model, u ai.Usage) ai.UsageCost {
	rates := model.Cost
	return ai.UsageCost{
		Input:      perMillion(u.Input, rates.Input),
		Output:     perMillion(u.Output, rates.Output),
		CacheRead:  perMillion(u.CacheRead, rates.CacheRead),
		CacheWrite: perMillion(u.CacheWrite, rates.CacheWrite),
	}
}

// perMillion prices tokens at rate, which is quoted per million tokens.
func perMillion(tokens int, rate float64) float64 {
	return float64(tokens) * rate / 1_000_000
}
