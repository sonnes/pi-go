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
	Message   json.RawMessage `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	CostUSD   float64         `json:"cost_usd,omitempty"`
	Usage     *rawUsage       `json:"usage,omitempty"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// anthropicMessage is the Anthropic API message format embedded in
// assistant event lines.
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

// toAIMessage converts an Anthropic API message to an [ai.Message].
func toAIMessage(msg anthropicMessage) ai.Message {
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
			Total:      msg.Usage.InputTokens + msg.Usage.OutputTokens,
		}
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
