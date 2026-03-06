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
