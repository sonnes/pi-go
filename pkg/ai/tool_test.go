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
