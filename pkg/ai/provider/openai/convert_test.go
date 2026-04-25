package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func TestConvertUserMessage_File(t *testing.T) {
	tests := []struct {
		name     string
		file     ai.File
		wantKey  string
		wantData string
	}{
		{
			name: "inline base64",
			file: ai.File{
				Data:     "base64pdfdata",
				MimeType: "application/pdf",
				Filename: "spec.pdf",
			},
			wantKey:  "file_data",
			wantData: "data:application/pdf;base64,base64pdfdata",
		},
		{
			name: "uploaded file id",
			file: ai.File{
				FileID: "file_abc123",
			},
			wantKey:  "file_id",
			wantData: "file_abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertUserMessage(ai.UserFileMessage("read this", tt.file))
			require.Len(t, result, 1)

			data, err := json.Marshal(result[0])
			require.NoError(t, err)

			var raw map[string]any
			require.NoError(t, json.Unmarshal(data, &raw))

			content, ok := raw["content"].([]any)
			require.True(t, ok, "content should be array")
			require.Len(t, content, 2)

			filePart := content[1].(map[string]any)
			assert.Equal(t, "file", filePart["type"])
			fileObj := filePart["file"].(map[string]any)
			assert.Equal(t, tt.wantData, fileObj[tt.wantKey])
		})
	}

	t.Run("URL-only file is skipped", func(t *testing.T) {
		result := convertUserMessage(ai.UserFileMessage("read this", ai.File{
			URL: "https://example.com/spec.pdf",
		}))
		require.Len(t, result, 1)

		data, err := json.Marshal(result[0])
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))

		content, ok := raw["content"].([]any)
		require.True(t, ok)
		assert.Len(t, content, 1)
	})
}
