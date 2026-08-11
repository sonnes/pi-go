package codex

import (
	"encoding/json"
	"fmt"

	"github.com/sonnes/pi-go/pkg/ai"
)

type rawLine struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id,omitempty"`
	Item     rawItem         `json:"item"`
	Usage    *rawUsage       `json:"usage,omitempty"`
	Message  string          `json:"message,omitempty"`
	Error    json.RawMessage `json:"error,omitempty"`
}

type rawItem struct {
	ID               string           `json:"id,omitempty"`
	Type             string           `json:"type,omitempty"`
	Text             string           `json:"text,omitempty"`
	Command          string           `json:"command,omitempty"`
	AggregatedOutput string           `json:"aggregated_output,omitempty"`
	ExitCode         *int             `json:"exit_code,omitempty"`
	Status           string           `json:"status,omitempty"`
	Query            string           `json:"query,omitempty"`
	Server           string           `json:"server,omitempty"`
	Tool             string           `json:"tool,omitempty"`
	Arguments        map[string]any   `json:"arguments,omitempty"`
	Result           json.RawMessage  `json:"result,omitempty"`
	Changes          []map[string]any `json:"changes,omitempty"`
	Items            []rawTodoItem    `json:"items,omitempty"`
}

type rawTodoItem struct {
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
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

// usageFromCodex converts a Codex CLI usage block to [ai.Usage]. It
// prices the usage with the rates of model.
//
// Codex relays the counts of OpenAI. In those counts, input_tokens
// includes the cached prefix, and output_tokens includes the reasoning
// tokens. This function separates both, so the categories are disjoint
// and a sum counts nothing twice. Reasoning bills at the output rate,
// because OpenAI charges it as output and prices no separate reasoning
// rate.
func usageFromCodex(model ai.Model, u rawUsage) ai.Usage {
	usage := ai.Usage{
		Input:     u.InputTokens - u.CachedInputTokens,
		Output:    u.OutputTokens - u.ReasoningOutputTokens,
		CacheRead: u.CachedInputTokens,
		Reasoning: u.ReasoningOutputTokens,
	}

	rates := model.Cost
	usage.Cost = ai.UsageCost{
		Input:     perMillion(usage.Input, rates.Input),
		Output:    perMillion(usage.Output, rates.Output),
		CacheRead: perMillion(usage.CacheRead, rates.CacheRead),
		Reasoning: perMillion(usage.Reasoning, rates.Output),
	}

	return usage
}

// perMillion prices tokens at rate, which is quoted per million tokens.
func perMillion(tokens int, rate float64) float64 {
	return float64(tokens) * rate / 1_000_000
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

func (item rawItem) todoArguments() map[string]any {
	todos := make([]map[string]any, 0, len(item.Items))
	for _, todo := range item.Items {
		status := "pending"
		if todo.Completed {
			status = "completed"
		}
		todos = append(todos, map[string]any{
			"content":     todo.Text,
			"active_form": todo.Text,
			"status":      status,
		})
	}
	return map[string]any{
		"todos": todos,
	}
}

func (item rawItem) outputText() string {
	if item.AggregatedOutput != "" {
		return item.AggregatedOutput
	}
	if len(item.Result) == 0 {
		return item.Status
	}
	var text string
	if err := json.Unmarshal(item.Result, &text); err == nil {
		return text
	}
	return string(item.Result)
}

func (item rawItem) serverToolCall() (ai.ToolCall, bool) {
	var (
		name       string
		serverType ai.ServerToolType
		arguments  map[string]any
	)

	switch item.Type {
	case "file_change":
		name = "file_change"
		serverType = ai.ServerToolTextEditor
		arguments = map[string]any{"changes": item.Changes}
	case "web_search", "web_search_call":
		name = "web_search"
		serverType = ai.ServerToolWebSearch
		arguments = item.Arguments
		if arguments == nil {
			arguments = map[string]any{}
		}
		if item.Query != "" {
			arguments["query"] = item.Query
		}
	case "mcp_tool_call":
		name = item.Tool
		if item.Server != "" && name != "" {
			name = item.Server + "." + name
		}
		if name == "" {
			name = "mcp"
		}
		serverType = ai.ServerToolMCP
		arguments = item.Arguments
	default:
		return ai.ToolCall{}, false
	}

	output := &ai.ServerToolOutput{
		Content: item.outputText(),
		IsError: item.commandFailed(),
	}
	if raw, err := json.Marshal(item); err == nil {
		output.Raw = raw
	}

	return ai.ToolCall{
		ID:         item.ID,
		Name:       name,
		Arguments:  arguments,
		Server:     true,
		ServerType: serverType,
		Output:     output,
	}, true
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
