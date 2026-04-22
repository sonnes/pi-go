package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
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
