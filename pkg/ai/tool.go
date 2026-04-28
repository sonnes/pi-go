package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// ToolKind distinguishes client-executed function tools from
// provider-executed server (built-in) tools. Branching logic compares
// against [ToolKindServer]; the empty zero value is treated as a
// function tool for backwards compatibility.
type ToolKind string

const (
	ToolKindFunction ToolKind = "function"
	ToolKindServer   ToolKind = "server"
)

// ServerToolType is the canonical pi-go identifier for a provider-hosted tool.
// Each provider adapter maps these to its own typed configuration.
type ServerToolType string

const (
	ServerToolWebSearch     ServerToolType = "web_search"
	ServerToolWebFetch      ServerToolType = "web_fetch"
	ServerToolCodeExecution ServerToolType = "code_execution"
	ServerToolComputer      ServerToolType = "computer"
	ServerToolBash          ServerToolType = "bash"
	ServerToolTextEditor    ServerToolType = "text_editor"
	ServerToolFileSearch    ServerToolType = "file_search"
	ServerToolToolSearch    ServerToolType = "tool_search"
	ServerToolMCP           ServerToolType = "mcp"
	ServerToolDateTime      ServerToolType = "datetime"
)

// ToolInfo contains tool metadata for model consumption.
type ToolInfo struct {
	Name string `json:"name"`
	// Description is the full tool documentation handed to the model
	// via the tool schema. It can run long.
	Description string `json:"description"`
	// UseWhen is a one-sentence hint describing when a caller should
	// reach for this tool. Used by system-prompt builders to list
	// tools concisely without dumping the full Description.
	UseWhen      string             `json:"use_when,omitempty"`
	InputSchema  *jsonschema.Schema `json:"input_schema"`
	OutputSchema *jsonschema.Schema `json:"output_schema,omitempty"`
	Parallel     bool               `json:"parallel,omitempty"`

	// Kind, ServerType, and ServerConfig are only meaningful when this
	// ToolInfo describes a provider-hosted server tool ([ToolKindServer]).
	// For function tools the zero values apply and these fields are ignored.
	Kind         ToolKind       `json:"kind,omitempty"`
	ServerType   ServerToolType `json:"server_type,omitempty"`
	ServerConfig map[string]any `json:"server_config,omitempty"`
}

// DefineServerTool wraps a [ToolInfo] describing a provider-hosted server
// tool into a [Tool]. The returned tool is advertised to the model through
// the same [Tool] interface as function tools, but is executed by the
// provider — calling Run on it always returns an error.
//
// Kind is forced to [ToolKindServer]; callers should populate Name,
// ServerType, and (optionally) ServerConfig:
//
//	ai.DefineServerTool(ai.ToolInfo{
//	    Name:       "web_search",
//	    ServerType: ai.ServerToolWebSearch,
//	    ServerConfig: map[string]any{"max_uses": 5},
//	})
func DefineServerTool(info ToolInfo) Tool {
	info.Kind = ToolKindServer
	if info.Name == "" {
		info.Name = string(info.ServerType)
	}
	return &serverToolImpl{info: info}
}

// serverToolImpl is a [Tool] adapter for provider-hosted tools. Run is never
// invoked by the agent for server tools — they're filtered out before
// execution because the provider has already produced the result inline.
type serverToolImpl struct {
	info ToolInfo
}

func (s *serverToolImpl) Info() ToolInfo { return s.info }

func (s *serverToolImpl) Run(_ context.Context, call ToolCallReq) (ToolResult, error) {
	return NewErrorResult(call.ID, fmt.Sprintf("server tool %q is provider-executed; client cannot invoke it", s.info.Name)), nil
}

// ToolCall represents a tool invocation from the model.
type ToolCallReq struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Input    string           `json:"input"` // JSON string
	OnUpdate func(ToolResult) `json:"-"`     // optional streaming progress callback
}

// ToolResult represents the result of tool execution.
type ToolResult struct {
	CallID    string `json:"call_id"`
	Type      string `json:"type"` // "text", "image", "media"
	Content   string `json:"content"`
	Data      []byte `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	IsError   bool   `json:"is_error"`
}

// NewTextResult creates a text tool result.
func NewTextResult(callID, content string) ToolResult {
	return ToolResult{
		CallID:  callID,
		Type:    "text",
		Content: content,
	}
}

// NewErrorResult creates an error tool result.
func NewErrorResult(callID, content string) ToolResult {
	return ToolResult{
		CallID:  callID,
		Type:    "text",
		Content: content,
		IsError: true,
	}
}

// NewImageResult creates an image tool result.
func NewImageResult(callID string, data []byte, mediaType string) ToolResult {
	return ToolResult{
		CallID:    callID,
		Type:      "image",
		Data:      data,
		MediaType: mediaType,
	}
}

// Tool is an executable tool that can be called by a model.
type Tool interface {
	Info() ToolInfo
	Run(ctx context.Context, call ToolCallReq) (ToolResult, error)
}

// ToolFunc is the function signature for typed tools.
type ToolFunc[In, Out any] func(ctx context.Context, input In) (Out, error)

// ToolDef is a tool definition with typed input and output.
type ToolDef[In, Out any] struct {
	name         string
	description  string
	useWhen      string
	fn           ToolFunc[In, Out]
	inputSchema  *jsonschema.Schema
	outputSchema *jsonschema.Schema
	parallel     bool
}

// DefineTool creates a typed tool with automatic schema generation.
func DefineTool[In, Out any](
	name, description string,
	fn ToolFunc[In, Out],
) *ToolDef[In, Out] {
	inputSchema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("failed to generate input schema for tool %s: %v", name, err))
	}
	outputSchema, err := jsonschema.For[Out](nil)
	if err != nil {
		panic(fmt.Sprintf("failed to generate output schema for tool %s: %v", name, err))
	}

	return &ToolDef[In, Out]{
		name:         name,
		description:  description,
		fn:           fn,
		inputSchema:  inputSchema,
		outputSchema: outputSchema,
	}
}

// DefineParallelTool creates a tool marked safe for parallel execution.
func DefineParallelTool[In, Out any](
	name, description string,
	fn ToolFunc[In, Out],
) *ToolDef[In, Out] {
	t := DefineTool(name, description, fn)
	t.parallel = true
	return t
}

// WithUseWhen attaches a one-sentence hint describing when this tool
// should be used. It is surfaced in [ToolInfo.UseWhen] for prompt
// builders that want a concise tool listing; the full [ToolDef]
// description is still passed to the model via the tool schema.
func (t *ToolDef[In, Out]) WithUseWhen(s string) *ToolDef[In, Out] {
	t.useWhen = s
	return t
}

// Info returns tool metadata.
func (t *ToolDef[In, Out]) Info() ToolInfo {
	return ToolInfo{
		Name:         t.name,
		Description:  t.description,
		UseWhen:      t.useWhen,
		InputSchema:  t.inputSchema,
		OutputSchema: t.outputSchema,
		Parallel:     t.parallel,
	}
}

// Run executes the tool with JSON input, returning [ToolResult].
func (t *ToolDef[In, Out]) Run(ctx context.Context, call ToolCallReq) (ToolResult, error) {
	var input In
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return NewErrorResult(call.ID, fmt.Sprintf("invalid input: %s", err)), nil
	}

	output, err := t.fn(ctx, input)
	if err != nil {
		return NewErrorResult(call.ID, err.Error()), nil
	}

	return marshalToolOutput(call.ID, output), nil
}

// marshalToolOutput converts typed output to [ToolResult].
func marshalToolOutput[Out any](callID string, output Out) ToolResult {
	switch v := any(output).(type) {
	case string:
		return NewTextResult(callID, v)
	case []byte:
		return ToolResult{
			CallID: callID,
			Type:   "media",
			Data:   v,
		}
	default:
		data, err := json.Marshal(output)
		if err != nil {
			return NewErrorResult(callID, fmt.Sprintf("failed to marshal output: %s", err))
		}
		return NewTextResult(callID, string(data))
	}
}
