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
