package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// ToolKind distinguishes function tools that the client runs from server
// (built-in) tools that the provider runs. Branching logic compares against
// [ToolKindServer]. For backward compatibility, the empty zero value counts
// as a function tool.
type ToolKind string

const (
	ToolKindFunction ToolKind = "function"
	ToolKindServer   ToolKind = "server"
)

// ServerToolType is the canonical pi-go identifier for a provider-hosted tool.
// Each provider adapter maps this identifier to its own typed configuration.
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

// ToolInfo contains the tool metadata that the model receives.
type ToolInfo struct {
	Name string `json:"name"`
	// Description is the full tool documentation. The tool schema gives
	// it to the model. The text can be long.
	Description string `json:"description"`
	// UseWhen is a one-sentence hint. It tells the caller when to use
	// this tool. System-prompt builders list tools with UseWhen, so
	// they do not include the full Description.
	UseWhen      string             `json:"use_when,omitempty"`
	InputSchema  *jsonschema.Schema `json:"input_schema"`
	OutputSchema *jsonschema.Schema `json:"output_schema,omitempty"`
	Parallel     bool               `json:"parallel,omitempty"`

	// Kind, ServerType, and ServerConfig are meaningful only when this
	// ToolInfo describes a provider-hosted server tool ([ToolKindServer]).
	// For function tools, the zero values apply and providers ignore
	// these fields.
	Kind         ToolKind       `json:"kind,omitempty"`
	ServerType   ServerToolType `json:"server_type,omitempty"`
	ServerConfig map[string]any `json:"server_config,omitempty"`
}

// DefineServerTool wraps a [ToolInfo] into a [Tool]. The ToolInfo describes a
// provider-hosted server tool. The model sees the returned tool through the
// same [Tool] interface as a function tool, but the provider runs it. A call
// to Run on the returned tool always returns an error.
//
// DefineServerTool forces Kind to [ToolKindServer]. The caller must set Name
// and ServerType, and can also set ServerConfig:
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

// serverToolImpl is a [Tool] adapter for provider-hosted tools. The agent
// never calls Run for a server tool. It filters server tools out before it
// runs any tool, because the provider already produced the result inline.
type serverToolImpl struct {
	info ToolInfo
}

func (s *serverToolImpl) Info() ToolInfo { return s.info }

func (s *serverToolImpl) Run(_ context.Context, call ToolCallReq) (ToolResult, error) {
	return NewErrorResult(call.ID, fmt.Sprintf("server tool %q is provider-executed; client cannot invoke it", s.info.Name)), nil
}

// ToolCallReq represents a tool invocation from the model.
type ToolCallReq struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Input    string           `json:"input"` // JSON string
	OnUpdate func(ToolResult) `json:"-"`     // optional streaming progress callback
}

// ToolResult represents the result of a tool run.
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

// Tool is a runnable tool that a model can call.
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

// DefineTool creates a typed tool. It generates the JSON schema
// automatically.
func DefineTool[In, Out any](
	name, description string,
	fn ToolFunc[In, Out],
) *ToolDef[In, Out] {
	inputSchema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("failed to generate input schema for tool %s: %v", name, err))
	}

	// A tool whose Out is [ToolResult] produces rich results (media,
	// images) that change on each call. An output schema has no meaning
	// for such a tool, so skip it and let fn return results directly.
	var outputSchema *jsonschema.Schema
	var zero Out
	if _, raw := any(zero).(ToolResult); !raw {
		outputSchema, err = jsonschema.For[Out](nil)
		if err != nil {
			panic(fmt.Sprintf("failed to generate output schema for tool %s: %v", name, err))
		}
	}

	return &ToolDef[In, Out]{
		name:         name,
		description:  description,
		fn:           fn,
		inputSchema:  inputSchema,
		outputSchema: outputSchema,
	}
}

// DefineParallelTool creates a tool that is safe to run in parallel.
func DefineParallelTool[In, Out any](
	name, description string,
	fn ToolFunc[In, Out],
) *ToolDef[In, Out] {
	t := DefineTool(name, description, fn)
	t.parallel = true
	return t
}

// WithUseWhen attaches a one-sentence hint. The hint tells the caller when
// to use this tool. It appears in [ToolInfo.UseWhen] for prompt builders
// that want a short tool listing. The tool schema still gives the full
// [ToolDef] description to the model.
func (t *ToolDef[In, Out]) WithUseWhen(s string) *ToolDef[In, Out] {
	t.useWhen = s
	return t
}

// WithOutputDescription sets a human-readable description on the generated
// output schema of the tool. The description says what the tool returns.
// WithOutputDescription copies the schema before it changes the copy, so a
// shared generated schema stays the same. It returns the def for chaining.
func (t *ToolDef[In, Out]) WithOutputDescription(s string) *ToolDef[In, Out] {
	if t.outputSchema != nil {
		schema := *t.outputSchema
		schema.Description = s
		t.outputSchema = &schema
	}
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

// Run runs the tool with JSON input and returns a [ToolResult].
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
	case ToolResult:
		// A tool that builds its own rich result (media, images). Stamp
		// the call ID, which overwrites any value that the tool set.
		// Then pass the result through unchanged.
		v.CallID = callID
		return v
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
