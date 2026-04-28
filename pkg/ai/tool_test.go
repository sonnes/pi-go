package ai_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

type addInput struct {
	A int `json:"a"`
	B int `json:"b"`
}

type addOutput struct {
	Sum int `json:"sum"`
}

func TestDefineTool(t *testing.T) {
	add := ai.DefineTool[addInput, addOutput](
		"add",
		"Add two numbers",
		func(_ context.Context, in addInput) (addOutput, error) {
			return addOutput{Sum: in.A + in.B}, nil
		},
	)

	info := add.Info()
	assert.Equal(t, "add", info.Name)
	assert.Equal(t, "Add two numbers", info.Description)
	assert.NotNil(t, info.InputSchema)
	assert.NotNil(t, info.OutputSchema)
	assert.False(t, info.Parallel)
}

func TestDefineParallelTool(t *testing.T) {
	add := ai.DefineParallelTool[addInput, addOutput](
		"add",
		"Add two numbers",
		func(_ context.Context, in addInput) (addOutput, error) {
			return addOutput{Sum: in.A + in.B}, nil
		},
	)

	assert.True(t, add.Info().Parallel)
}

func TestToolDef_Run(t *testing.T) {
	add := ai.DefineTool[addInput, addOutput](
		"add",
		"Add two numbers",
		func(_ context.Context, in addInput) (addOutput, error) {
			return addOutput{Sum: in.A + in.B}, nil
		},
	)

	t.Run("valid input", func(t *testing.T) {
		result, err := add.Run(context.Background(), ai.ToolCallReq{
			ID:    "call-1",
			Name:  "add",
			Input: `{"a": 3, "b": 4}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "call-1", result.CallID)
		assert.Equal(t, "text", result.Type)
		assert.False(t, result.IsError)

		var out addOutput
		err = json.Unmarshal([]byte(result.Content), &out)
		require.NoError(t, err)
		assert.Equal(t, 7, out.Sum)
	})

	t.Run("invalid input", func(t *testing.T) {
		result, err := add.Run(context.Background(), ai.ToolCallReq{
			ID:    "call-2",
			Name:  "add",
			Input: `not json`,
		})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "invalid input")
	})
}

func TestToolDef_Run_StringOutput(t *testing.T) {
	greet := ai.DefineTool[addInput, string](
		"greet",
		"Greet",
		func(_ context.Context, in addInput) (string, error) {
			return "hello", nil
		},
	)

	result, err := greet.Run(context.Background(), ai.ToolCallReq{
		ID:    "call-1",
		Name:  "greet",
		Input: `{"a": 1, "b": 2}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "text", result.Type)
	assert.Equal(t, "hello", result.Content)
	assert.False(t, result.IsError)
}

func TestToolDef_Run_Error(t *testing.T) {
	fail := ai.DefineTool[addInput, string](
		"fail",
		"Always fails",
		func(_ context.Context, in addInput) (string, error) {
			return "", assert.AnError
		},
	)

	result, err := fail.Run(context.Background(), ai.ToolCallReq{
		ID:    "call-1",
		Name:  "fail",
		Input: `{"a": 1, "b": 2}`,
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, assert.AnError.Error())
}

func TestNewTextResult(t *testing.T) {
	r := ai.NewTextResult("id-1", "hello")
	assert.Equal(t, "id-1", r.CallID)
	assert.Equal(t, "text", r.Type)
	assert.Equal(t, "hello", r.Content)
	assert.False(t, r.IsError)
}

func TestNewErrorResult(t *testing.T) {
	r := ai.NewErrorResult("id-1", "boom")
	assert.Equal(t, "id-1", r.CallID)
	assert.Equal(t, "text", r.Type)
	assert.Equal(t, "boom", r.Content)
	assert.True(t, r.IsError)
}

func TestNewImageResult(t *testing.T) {
	r := ai.NewImageResult("id-1", []byte("pixels"), "image/png")
	assert.Equal(t, "id-1", r.CallID)
	assert.Equal(t, "image", r.Type)
	assert.Equal(t, []byte("pixels"), r.Data)
	assert.Equal(t, "image/png", r.MediaType)
	assert.False(t, r.IsError)
}

func TestToolInfo_ZeroKindIsNotServer(t *testing.T) {
	// The zero-value ToolKind ("") is treated as a function tool —
	// branching logic compares against ToolKindServer, not Function.
	info := ai.ToolInfo{Name: "x"}
	assert.NotEqual(t, ai.ToolKindServer, info.Kind)
}

func TestDefineServerTool(t *testing.T) {
	t.Run("forces Kind=Server and defaults Name from ServerType", func(t *testing.T) {
		tool := ai.DefineServerTool(ai.ToolInfo{
			ServerType:   ai.ServerToolWebSearch,
			ServerConfig: map[string]any{"max_uses": 2},
		})

		info := tool.Info()
		assert.Equal(t, "web_search", info.Name)
		assert.Equal(t, ai.ToolKindServer, info.Kind)
		assert.Equal(t, ai.ServerToolWebSearch, info.ServerType)
		assert.Equal(t, 2, info.ServerConfig["max_uses"])
	})

	t.Run("preserves explicit Name when set", func(t *testing.T) {
		tool := ai.DefineServerTool(ai.ToolInfo{
			Name:       "google_search",
			ServerType: ai.ServerToolWebSearch,
		})
		assert.Equal(t, "google_search", tool.Info().Name)
	})

	t.Run("Run always returns an error result", func(t *testing.T) {
		// Server tools are provider-executed; the agent filters them
		// before reaching Run, but defense-in-depth still matters.
		tool := ai.DefineServerTool(ai.ToolInfo{
			ServerType: ai.ServerToolWebSearch,
		})

		res, err := tool.Run(context.Background(), ai.ToolCallReq{
			ID:   "x",
			Name: "web_search",
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Content, "provider-executed")
	})
}
