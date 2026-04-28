package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func TestConvertUserContent_File(t *testing.T) {
	tests := []struct {
		name        string
		file        ai.File
		wantBlock   bool
		wantSrcType string
		wantData    string // expected source.data after marshal; "" to skip
	}{
		{
			name: "base64 PDF",
			file: ai.File{
				Data:     "base64pdf",
				MimeType: "application/pdf",
			},
			wantBlock:   true,
			wantSrcType: "base64",
			wantData:    "base64pdf",
		},
		{
			name: "plain text decodes base64 to raw",
			file: ai.File{
				Data:     base64.StdEncoding.EncodeToString([]byte("hello")),
				MimeType: "text/plain",
			},
			wantBlock:   true,
			wantSrcType: "text",
			wantData:    "hello",
		},
		{
			name: "plain text with invalid base64 is skipped",
			file: ai.File{
				Data:     "not!valid!base64!",
				MimeType: "text/plain",
			},
			wantBlock: false,
		},
		{
			name: "URL PDF",
			file: ai.File{
				URL: "https://example.com/spec.pdf",
			},
			wantBlock:   true,
			wantSrcType: "url",
		},
		{
			name: "unsupported mime type",
			file: ai.File{
				Data:     "abc",
				MimeType: "image/png",
			},
			wantBlock: false,
		},
		{
			name: "FileID alone is unsupported",
			file: ai.File{
				FileID: "file_abc",
			},
			wantBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := convertUserContent([]ai.Content{tt.file})
			if !tt.wantBlock {
				assert.Empty(t, blocks)
				return
			}

			require.Len(t, blocks, 1)
			require.NotNil(t, blocks[0].OfDocument)

			data, err := json.Marshal(blocks[0])
			require.NoError(t, err)

			var raw map[string]any
			require.NoError(t, json.Unmarshal(data, &raw))

			assert.Equal(t, "document", raw["type"])
			source := raw["source"].(map[string]any)
			assert.Equal(t, tt.wantSrcType, source["type"])
			if tt.wantData != "" {
				assert.Equal(t, tt.wantData, source["data"])
			}
		})
	}
}

func TestConvertTools_ServerWebSearch(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:       "web_search",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolWebSearch,
			ServerConfig: map[string]any{
				"max_uses":        3,
				"allowed_domains": []string{"example.com"},
			},
		},
	}

	result := convertTools(tools)
	require.Len(t, result, 1)

	ws := result[0].OfWebSearchTool20250305
	require.NotNil(t, ws, "expected OfWebSearchTool20250305 to be set")

	body, err := json.Marshal(ws)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))

	assert.Equal(t, "web_search", got["name"])
	assert.Equal(t, "web_search_20250305", got["type"])
	assert.EqualValues(t, 3, got["max_uses"])
	assert.Equal(t, []any{"example.com"}, got["allowed_domains"])
}

func TestConvertTools_ServerToolUnsupportedSkipped(t *testing.T) {
	// code_execution is not supported on the non-beta API path; convertTools
	// should drop it silently rather than error or pass it as a function tool.
	tools := []ai.ToolInfo{
		{
			Name:       "code_execution",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolCodeExecution,
		},
	}

	result := convertTools(tools)
	assert.Empty(t, result, "unsupported server tool should be skipped")
}

func TestConvertTools_MixedFunctionAndServer(t *testing.T) {
	tools := []ai.ToolInfo{
		{
			Name:        "get_weather",
			Description: "Get the weather",
		},
		{
			Name:       "web_search",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolWebSearch,
		},
	}

	result := convertTools(tools)
	require.Len(t, result, 2)

	require.NotNil(t, result[0].OfTool)
	assert.Equal(t, "get_weather", result[0].OfTool.Name)

	require.NotNil(t, result[1].OfWebSearchTool20250305)
}

func TestServerTypeForName(t *testing.T) {
	assert.Equal(t, ai.ServerToolWebSearch, serverTypeForName("web_search"))
	assert.Equal(t, ai.ServerToolType("unknown"), serverTypeForName("unknown"))
}

func TestServerToolInputToMap(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		assert.Nil(t, serverToolInputToMap(nil))
	})
	t.Run("already a map", func(t *testing.T) {
		in := map[string]any{"query": "weather sf"}
		assert.Equal(t, in, serverToolInputToMap(in))
	})
	t.Run("struct via json", func(t *testing.T) {
		in := struct {
			Query string `json:"query"`
		}{Query: "weather sf"}
		got := serverToolInputToMap(in)
		assert.Equal(t, "weather sf", got["query"])
	})
}
