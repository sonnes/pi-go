package google

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func TestConvertUserParts_File(t *testing.T) {
	t.Run("inline base64", func(t *testing.T) {
		parts := convertUserParts([]ai.Content{
			ai.File{
				Data:     "dGVzdA==",
				MimeType: "application/pdf",
			},
		})
		require.Len(t, parts, 1)
		require.NotNil(t, parts[0].InlineData)
		assert.Equal(t, "application/pdf", parts[0].InlineData.MIMEType)
	})

	t.Run("URL reference", func(t *testing.T) {
		parts := convertUserParts([]ai.Content{
			ai.File{
				URL:      "gs://bucket/spec.pdf",
				MimeType: "application/pdf",
				Filename: "spec.pdf",
			},
		})
		require.Len(t, parts, 1)
		require.NotNil(t, parts[0].FileData)
		assert.Equal(t, "gs://bucket/spec.pdf", parts[0].FileData.FileURI)
		assert.Equal(t, "application/pdf", parts[0].FileData.MIMEType)
		assert.Equal(t, "spec.pdf", parts[0].FileData.DisplayName)
	})

	t.Run("FileID reference", func(t *testing.T) {
		parts := convertUserParts([]ai.Content{
			ai.File{
				FileID:   "files/abc123",
				MimeType: "application/pdf",
			},
		})
		require.Len(t, parts, 1)
		require.NotNil(t, parts[0].FileData)
		assert.Equal(t, "files/abc123", parts[0].FileData.FileURI)
	})

	t.Run("invalid base64 skipped", func(t *testing.T) {
		parts := convertUserParts([]ai.Content{
			ai.File{
				Data:     "not!base64",
				MimeType: "application/pdf",
			},
		})
		assert.Empty(t, parts)
	})
}

func TestConvertTools_ServerGoogleSearch(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:       "web_search",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolWebSearch,
		},
	}

	googleTools, _ := convertTools(tools, ai.ToolChoiceAuto)
	require.Len(t, googleTools, 1)
	assert.NotNil(t, googleTools[0].GoogleSearch)
	assert.Nil(t, googleTools[0].CodeExecution)
	assert.Empty(t, googleTools[0].FunctionDeclarations)
}

func TestConvertTools_ServerCodeExecution(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:       "code_execution",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolCodeExecution,
		},
	}

	googleTools, _ := convertTools(tools, ai.ToolChoiceAuto)
	require.Len(t, googleTools, 1)
	assert.NotNil(t, googleTools[0].CodeExecution)
	assert.Nil(t, googleTools[0].GoogleSearch)
}

func TestConvertTools_FunctionAndServerInSeparateToolEntries(t *testing.T) {
	// Gemini does not accept FunctionDeclarations together with google_search
	// or code_execution in the same Tool entry. They must be in separate
	// entries.
	tools := []ai.ToolInfo{
		{Name: "get_weather", Description: "Get weather"},
		{
			Name:       "web_search",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolWebSearch,
		},
	}

	googleTools, _ := convertTools(tools, ai.ToolChoiceAuto)
	require.Len(t, googleTools, 2)

	assert.Len(t, googleTools[0].FunctionDeclarations, 1)
	assert.Equal(t, "get_weather", googleTools[0].FunctionDeclarations[0].Name)
	assert.Nil(t, googleTools[0].GoogleSearch)

	assert.NotNil(t, googleTools[1].GoogleSearch)
	assert.Empty(t, googleTools[1].FunctionDeclarations)
}

func TestConvertTools_CurrentGeminiServerTools(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:       "web_fetch",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolWebFetch,
		},
		{
			Name:       "file_search",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolFileSearch,
			ServerConfig: map[string]any{
				"store_names": []string{"fileSearchStores/pi-go"},
				"top_k":       4,
			},
		},
		{
			Name:       "computer",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolComputer,
			ServerConfig: map[string]any{
				"environment": "browser",
			},
		},
	}

	googleTools, _ := convertTools(tools, ai.ToolChoiceAuto)
	require.Len(t, googleTools, 1)

	serverTool := googleTools[0]
	assert.NotNil(t, serverTool.URLContext)
	require.NotNil(t, serverTool.FileSearch)
	assert.Equal(t, []string{"fileSearchStores/pi-go"}, serverTool.FileSearch.FileSearchStoreNames)
	require.NotNil(t, serverTool.FileSearch.TopK)
	assert.Equal(t, int32(4), *serverTool.FileSearch.TopK)
	require.NotNil(t, serverTool.ComputerUse)
	assert.Equal(t, "ENVIRONMENT_BROWSER", string(serverTool.ComputerUse.Environment))
}
