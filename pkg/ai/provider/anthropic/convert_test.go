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
