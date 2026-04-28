package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Role indicates which entity produced a message.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "tool_result"
)

// Message represents a message in a conversation.
// Role-specific fields are zero-valued when not applicable.
type Message struct {
	Role    Role
	Content []Content

	// Assistant-specific fields
	API        string
	Provider   string
	Model      string
	Usage      Usage
	StopReason StopReason
	Error      string

	// ToolResult-specific fields
	ToolCallID string
	ToolName   string
	IsError    bool

	Timestamp time.Time
}

// UserMessage creates a user message with text content.
func UserMessage(text string) Message {
	return Message{
		Role:      RoleUser,
		Content:   []Content{Text{Text: text}},
		Timestamp: time.Now(),
	}
}

// UserImageMessage creates a user message with text and image content.
func UserImageMessage(text string, images ...Image) Message {
	content := make([]Content, 0, 1+len(images))
	content = append(content, Text{Text: text})
	for _, img := range images {
		content = append(content, img)
	}
	return Message{
		Role:      RoleUser,
		Content:   content,
		Timestamp: time.Now(),
	}
}

// UserFileMessage creates a user message with text and file/document content.
func UserFileMessage(text string, files ...File) Message {
	content := make([]Content, 0, 1+len(files))
	content = append(content, Text{Text: text})
	for _, f := range files {
		content = append(content, f)
	}
	return Message{
		Role:      RoleUser,
		Content:   content,
		Timestamp: time.Now(),
	}
}

// AssistantMessage creates an assistant message with the given content blocks.
func AssistantMessage(content ...Content) Message {
	return Message{
		Role:      RoleAssistant,
		Content:   content,
		Timestamp: time.Now(),
	}
}

// ToolResultMessage creates a tool result message.
func ToolResultMessage(toolCallID, toolName string, content ...Content) Message {
	return Message{
		Role:       RoleToolResult,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Content:    content,
		Timestamp:  time.Now(),
	}
}

// ErrorToolResultMessage creates a tool result message indicating an error.
func ErrorToolResultMessage(toolCallID, toolName, errMsg string) Message {
	return Message{
		Role:       RoleToolResult,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Content:    []Content{Text{Text: errMsg}},
		IsError:    true,
		Timestamp:  time.Now(),
	}
}

// Text returns the concatenated text of all [Text] content blocks.
func (m Message) Text() string {
	var sb strings.Builder
	for _, c := range m.Content {
		if t, ok := AsContent[Text](c); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

// ToolCalls returns all [ToolCall] content blocks, or nil if none exist.
func (m Message) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, c := range m.Content {
		if tc, ok := AsContent[ToolCall](c); ok {
			calls = append(calls, tc)
		}
	}
	return calls
}

// String returns a short debug representation of the message.
// Text is truncated to 100 characters.
func (m Message) String() string {
	const maxLen = 100

	text := m.Text()
	if len(text) > maxLen {
		text = text[:maxLen] + "..."
	}

	calls := m.ToolCalls()

	switch {
	case m.Role == RoleToolResult && m.IsError:
		return fmt.Sprintf("tool_result(%s) ERROR: %s", m.ToolName, text)
	case m.Role == RoleToolResult:
		return fmt.Sprintf("tool_result(%s): %s", m.ToolName, text)
	case len(calls) > 0 && text != "":
		names := toolCallNames(calls)
		return fmt.Sprintf("%s: %s [tool_calls: %s]", m.Role, text, names)
	case len(calls) > 0:
		names := toolCallNames(calls)
		return fmt.Sprintf("%s: [tool_calls: %s]", m.Role, names)
	case text != "":
		return fmt.Sprintf("%s: %s", m.Role, text)
	default:
		return fmt.Sprintf("%s:", m.Role)
	}
}

func toolCallNames(calls []ToolCall) string {
	names := make([]string, len(calls))
	for i, tc := range calls {
		names[i] = tc.Name
	}
	return strings.Join(names, ", ")
}

// --- JSON marshaling ---

type messageJSON struct {
	Role       Role          `json:"role"`
	Content    []contentJSON `json:"content"`
	API        string        `json:"api,omitempty"`
	Provider   string        `json:"provider,omitempty"`
	Model      string        `json:"model,omitempty"`
	Usage      *usageJSON    `json:"usage,omitempty"`
	StopReason StopReason    `json:"stop_reason,omitempty"`
	Error      string        `json:"error,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolName   string        `json:"tool_name,omitempty"`
	IsError    bool          `json:"is_error,omitempty"`
	Timestamp  string        `json:"timestamp,omitempty"`
}

type contentJSON struct {
	Type string `json:"type"`

	// text / text signature
	Text      string `json:"text,omitempty"`
	Signature string `json:"signature,omitempty"`

	// thinking
	Thinking string `json:"thinking,omitempty"`

	// image / file
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mime_type,omitempty"`

	// file
	URL      string `json:"url,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	Filename string `json:"filename,omitempty"`

	// tool_call
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`

	// tool_call (server-side variants only)
	Server       bool                  `json:"server,omitempty"`
	ServerType   ServerToolType        `json:"server_type,omitempty"`
	ServerOutput *serverToolOutputJSON `json:"output,omitempty"`
}

type serverToolOutputJSON struct {
	Content string          `json:"content,omitempty"`
	Raw     json.RawMessage `json:"raw,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

type usageJSON struct {
	Input      int       `json:"input,omitempty"`
	Output     int       `json:"output,omitempty"`
	CacheRead  int       `json:"cache_read,omitempty"`
	CacheWrite int       `json:"cache_write,omitempty"`
	Total      int       `json:"total,omitempty"`
	Cost       *costJSON `json:"cost,omitempty"`
}

type costJSON struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
	Total      float64 `json:"total,omitempty"`
}

// MarshalJSON encodes Message to JSON.
func (m Message) MarshalJSON() ([]byte, error) {
	j := messageJSON{
		Role:       m.Role,
		API:        m.API,
		Provider:   m.Provider,
		Model:      m.Model,
		StopReason: m.StopReason,
		Error:      m.Error,
		ToolCallID: m.ToolCallID,
		ToolName:   m.ToolName,
		IsError:    m.IsError,
	}

	if !m.Timestamp.IsZero() {
		j.Timestamp = m.Timestamp.UTC().Format(time.RFC3339Nano)
	}

	for _, c := range m.Content {
		j.Content = append(j.Content, marshalContent(c))
	}

	if m.Usage != (Usage{}) {
		u := &usageJSON{
			Input:      m.Usage.Input,
			Output:     m.Usage.Output,
			CacheRead:  m.Usage.CacheRead,
			CacheWrite: m.Usage.CacheWrite,
			Total:      m.Usage.Total,
		}
		if m.Usage.Cost != (UsageCost{}) {
			u.Cost = &costJSON{
				Input:      m.Usage.Cost.Input,
				Output:     m.Usage.Cost.Output,
				CacheRead:  m.Usage.Cost.CacheRead,
				CacheWrite: m.Usage.Cost.CacheWrite,
				Total:      m.Usage.Cost.Total,
			}
		}
		j.Usage = u
	}

	return json.Marshal(j)
}

// UnmarshalJSON decodes Message from JSON.
func (m *Message) UnmarshalJSON(data []byte) error {
	var j messageJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}

	m.Role = j.Role
	m.API = j.API
	m.Provider = j.Provider
	m.Model = j.Model
	m.StopReason = j.StopReason
	m.Error = j.Error
	m.ToolCallID = j.ToolCallID
	m.ToolName = j.ToolName
	m.IsError = j.IsError

	if j.Timestamp != "" {
		t, err := time.Parse(time.RFC3339Nano, j.Timestamp)
		if err == nil {
			m.Timestamp = t
		}
	}

	for _, c := range j.Content {
		block, err := unmarshalContent(c)
		if err != nil {
			return fmt.Errorf("unmarshaling content: %w", err)
		}
		m.Content = append(m.Content, block)
	}

	if j.Usage != nil {
		m.Usage = Usage{
			Input:      j.Usage.Input,
			Output:     j.Usage.Output,
			CacheRead:  j.Usage.CacheRead,
			CacheWrite: j.Usage.CacheWrite,
			Total:      j.Usage.Total,
		}
		if j.Usage.Cost != nil {
			m.Usage.Cost = UsageCost{
				Input:      j.Usage.Cost.Input,
				Output:     j.Usage.Cost.Output,
				CacheRead:  j.Usage.Cost.CacheRead,
				CacheWrite: j.Usage.Cost.CacheWrite,
				Total:      j.Usage.Cost.Total,
			}
		}
	}

	return nil
}

func marshalContent(c Content) contentJSON {
	switch v := c.(type) {
	case Text:
		return contentJSON{
			Type:      "text",
			Text:      v.Text,
			Signature: v.Signature,
		}
	case Thinking:
		return contentJSON{
			Type:      "thinking",
			Thinking:  v.Thinking,
			Signature: v.Signature,
		}
	case Image:
		return contentJSON{
			Type:     "image",
			Data:     v.Data,
			MimeType: v.MimeType,
		}
	case File:
		return contentJSON{
			Type:     "file",
			Data:     v.Data,
			URL:      v.URL,
			FileID:   v.FileID,
			MimeType: v.MimeType,
			Filename: v.Filename,
		}
	case ToolCall:
		out := contentJSON{
			Type:       "tool_call",
			ID:         v.ID,
			Name:       v.Name,
			Arguments:  v.Arguments,
			Signature:  v.Signature,
			Server:     v.Server,
			ServerType: v.ServerType,
		}
		if v.Output != nil {
			out.ServerOutput = &serverToolOutputJSON{
				Content: v.Output.Content,
				Raw:     v.Output.Raw,
				IsError: v.Output.IsError,
			}
		}
		return out
	default:
		return contentJSON{Type: "unknown"}
	}
}

func unmarshalContent(c contentJSON) (Content, error) {
	switch c.Type {
	case "text":
		return Text{
			Text:      c.Text,
			Signature: c.Signature,
		}, nil
	case "thinking":
		return Thinking{
			Thinking:  c.Thinking,
			Signature: c.Signature,
		}, nil
	case "image":
		return Image{
			Data:     c.Data,
			MimeType: c.MimeType,
		}, nil
	case "file":
		return File{
			Data:     c.Data,
			URL:      c.URL,
			FileID:   c.FileID,
			MimeType: c.MimeType,
			Filename: c.Filename,
		}, nil
	case "tool_call":
		tc := ToolCall{
			ID:         c.ID,
			Name:       c.Name,
			Arguments:  c.Arguments,
			Signature:  c.Signature,
			Server:     c.Server,
			ServerType: c.ServerType,
		}
		if c.ServerOutput != nil {
			tc.Output = &ServerToolOutput{
				Content: c.ServerOutput.Content,
				Raw:     c.ServerOutput.Raw,
				IsError: c.ServerOutput.IsError,
			}
		}
		return tc, nil
	default:
		return nil, fmt.Errorf("unknown content type: %s", c.Type)
	}
}
