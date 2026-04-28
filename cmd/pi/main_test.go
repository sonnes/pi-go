package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestParseServerTools_Empty(t *testing.T) {
	tools, err := parseServerTools("")
	require.NoError(t, err)
	assert.Nil(t, tools)
}

func TestParseServerTools_KnownNames(t *testing.T) {
	tools, err := parseServerTools("web_search,code_execution")
	require.NoError(t, err)
	require.Len(t, tools, 2)

	assert.Equal(t, ai.ToolKindServer, tools[0].Info().Kind)
	assert.Equal(t, ai.ServerToolWebSearch, tools[0].Info().ServerType)

	assert.Equal(t, ai.ToolKindServer, tools[1].Info().Kind)
	assert.Equal(t, ai.ServerToolCodeExecution, tools[1].Info().ServerType)
}

func TestParseServerTools_TrimsWhitespaceAndSkipsEmpties(t *testing.T) {
	tools, err := parseServerTools(" web_search , , code_execution ")
	require.NoError(t, err)
	require.Len(t, tools, 2)
	assert.Equal(t, ai.ServerToolWebSearch, tools[0].Info().ServerType)
	assert.Equal(t, ai.ServerToolCodeExecution, tools[1].Info().ServerType)
}

func TestParseServerTools_UnknownName(t *testing.T) {
	tools, err := parseServerTools("web_search,frobulate")
	assert.Nil(t, tools)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown server tool "frobulate"`)
}

func TestParseServerTools_AllRecognizedTypes(t *testing.T) {
	spec := "web_search,code_execution,web_fetch,file_search,computer,bash,text_editor,tool_search,mcp"
	tools, err := parseServerTools(spec)
	require.NoError(t, err)
	require.Len(t, tools, 9)

	want := []ai.ServerToolType{
		ai.ServerToolWebSearch,
		ai.ServerToolCodeExecution,
		ai.ServerToolWebFetch,
		ai.ServerToolFileSearch,
		ai.ServerToolComputer,
		ai.ServerToolBash,
		ai.ServerToolTextEditor,
		ai.ServerToolToolSearch,
		ai.ServerToolMCP,
	}
	for i, w := range want {
		assert.Equal(t, w, tools[i].Info().ServerType, "index %d", i)
	}
}
