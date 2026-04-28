package ai_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sonnes/pi-go/pkg/ai"
)

func TestAsContent(t *testing.T) {
	tests := []struct {
		name    string
		content ai.Content
		wantOK  bool
	}{
		{
			name:    "text from Text",
			content: ai.Text{Text: "hello"},
			wantOK:  true,
		},
		{
			name:    "text from Thinking",
			content: ai.Thinking{Thinking: "hmm"},
			wantOK:  false,
		},
		{
			name:    "nil content",
			content: nil,
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := ai.AsContent[ai.Text](tt.content)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestAsContent_File(t *testing.T) {
	c := ai.Content(ai.File{
		Data:     "base64data",
		MimeType: "application/pdf",
		Filename: "report.pdf",
	})

	f, ok := ai.AsContent[ai.File](c)
	assert.True(t, ok)
	assert.Equal(t, "base64data", f.Data)
	assert.Equal(t, "application/pdf", f.MimeType)
	assert.Equal(t, "report.pdf", f.Filename)
}

func TestToolCall_ServerVariant(t *testing.T) {
	c := ai.Content(ai.ToolCall{
		ID:         "srvtoolu_01",
		Name:       "web_search",
		Server:     true,
		ServerType: ai.ServerToolWebSearch,
		Arguments:  map[string]any{"query": "weather sf"},
		Output: &ai.ServerToolOutput{
			Content: "1. example.com — sunny\n",
		},
	})

	tc, ok := ai.AsContent[ai.ToolCall](c)
	assert.True(t, ok)
	assert.True(t, tc.Server)
	assert.Equal(t, ai.ServerToolWebSearch, tc.ServerType)
	assert.NotNil(t, tc.Output)
	assert.Equal(t, "1. example.com — sunny\n", tc.Output.Content)
	assert.False(t, tc.Output.IsError)
}

func TestToolCall_FunctionVariant_DefaultsToNonServer(t *testing.T) {
	tc := ai.ToolCall{ID: "id", Name: "fn"}
	assert.False(t, tc.Server)
	assert.Equal(t, ai.ServerToolType(""), tc.ServerType)
	assert.Nil(t, tc.Output)
}
