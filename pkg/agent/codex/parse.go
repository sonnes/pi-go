package codex

import (
	"encoding/json"
	"fmt"

	"github.com/sonnes/pi-go/pkg/ai"
)

type rawLine struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id,omitempty"`
	Item     rawItem         `json:"item,omitempty"`
	Usage    *rawUsage       `json:"usage,omitempty"`
	Message  string          `json:"message,omitempty"`
	Error    json.RawMessage `json:"error,omitempty"`
}

type rawItem struct {
	ID               string `json:"id,omitempty"`
	Type             string `json:"type,omitempty"`
	Text             string `json:"text,omitempty"`
	Command          string `json:"command,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	Status           string `json:"status,omitempty"`
}

type rawUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

func parseLine(data []byte) (rawLine, error) {
	var line rawLine
	err := json.Unmarshal(data, &line)
	return line, err
}

func usageFromCodex(u rawUsage) ai.Usage {
	return ai.Usage{
		Input:     u.InputTokens,
		Output:    u.OutputTokens,
		CacheRead: u.CachedInputTokens,
		Reasoning: u.ReasoningOutputTokens,
		Total:     u.InputTokens + u.OutputTokens,
	}
}

func (item rawItem) commandFailed() bool {
	if item.ExitCode != nil && *item.ExitCode != 0 {
		return true
	}
	switch item.Status {
	case "", "completed", "success":
		return false
	default:
		return true
	}
}

func (line rawLine) error() error {
	if line.Message != "" {
		return fmt.Errorf("codex: %s", line.Message)
	}
	if len(line.Error) == 0 {
		return fmt.Errorf("codex: %s", line.Type)
	}
	var s string
	if err := json.Unmarshal(line.Error, &s); err == nil && s != "" {
		return fmt.Errorf("codex: %s", s)
	}
	var obj struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(line.Error, &obj); err == nil && obj.Message != "" {
		return fmt.Errorf("codex: %s", obj.Message)
	}
	return fmt.Errorf("codex: %s", string(line.Error))
}
