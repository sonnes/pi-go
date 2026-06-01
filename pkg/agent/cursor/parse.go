package cursor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sonnes/pi-go/pkg/ai"
)

type rawLine struct {
	Type      string      `json:"type"`
	Subtype   string      `json:"subtype,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Model     string      `json:"model,omitempty"`
	Message   rawMessage  `json:"message,omitempty"`
	Result    string      `json:"result,omitempty"`
	IsError   bool        `json:"is_error,omitempty"`
	CallID    string      `json:"call_id,omitempty"`
	ToolCall  rawToolCall `json:"tool_call,omitempty"`
}

type rawMessage struct {
	Role    string       `json:"role,omitempty"`
	Content []rawContent `json:"content,omitempty"`
}

type rawContent struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

type rawToolCall map[string]rawToolPayload

type rawToolPayload struct {
	Args   map[string]any  `json:"args,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

type toolInfo struct {
	ID         string
	Name       string
	Args       map[string]any
	Result     string
	IsError    bool
	Raw        json.RawMessage
	ServerType ai.ServerToolType
}

func parseLine(data []byte) (rawLine, error) {
	var line rawLine
	err := json.Unmarshal(data, &line)
	return line, err
}

func (m rawMessage) text() string {
	var sb strings.Builder
	for _, c := range m.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

func (line rawLine) err() error {
	if line.Result != "" {
		return fmt.Errorf("cursor: %s", line.Result)
	}
	if line.Subtype != "" {
		return fmt.Errorf("cursor: %s/%s", line.Type, line.Subtype)
	}
	return fmt.Errorf("cursor: %s", line.Type)
}

func (line rawLine) toolInfo() (toolInfo, bool) {
	for key, payload := range line.ToolCall {
		name := toolName(key)
		result, isError := toolResultText(payload.Result)
		raw, _ := json.Marshal(line.ToolCall)
		return toolInfo{
			ID:         line.CallID,
			Name:       name,
			Args:       payload.Args,
			Result:     result,
			IsError:    isError,
			Raw:        raw,
			ServerType: serverTypeForTool(name),
		}, true
	}
	return toolInfo{}, false
}

func toolName(key string) string {
	base := strings.TrimSuffix(key, "ToolCall")
	lower := strings.ToLower(base)
	switch {
	case strings.Contains(lower, "terminal"),
		strings.Contains(lower, "command"),
		strings.Contains(lower, "shell"):
		return "bash"
	case strings.Contains(lower, "read"):
		return "read"
	case strings.Contains(lower, "write"):
		return "write"
	case strings.Contains(lower, "edit"):
		return "edit"
	default:
		return base
	}
}

func serverTypeForTool(name string) ai.ServerToolType {
	switch name {
	case "bash":
		return ai.ServerToolBash
	case "read", "write", "edit":
		return ai.ServerToolTextEditor
	default:
		return ai.ServerToolType("")
	}
}

func toolResultText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}

	var wrapped struct {
		Success json.RawMessage `json:"success,omitempty"`
		Error   json.RawMessage `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		if len(wrapped.Error) > 0 {
			return rawMessageText(wrapped.Error), true
		}
		if len(wrapped.Success) > 0 {
			return successText(wrapped.Success), false
		}
	}

	return rawMessageText(raw), false
}

func successText(raw json.RawMessage) string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return rawMessageText(raw)
	}
	for _, key := range []string{"content", "output", "stdout", "stderr", "message", "path"} {
		if v, ok := fields[key].(string); ok && v != "" {
			return v
		}
	}
	return rawMessageText(raw)
}

func rawMessageText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var obj struct {
		Message string `json:"message"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.Message != "" {
			return obj.Message
		}
		if obj.Content != "" {
			return obj.Content
		}
	}

	return string(raw)
}
