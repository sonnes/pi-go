package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

func TestImageModelID(t *testing.T) {
	assert.Equal(t, "gpt-image-2", GPTImage2.ID)
	assert.Equal(t, "gpt-image-2", imageModelID(ai.Model{}))
	assert.Equal(t, "gpt-image-2", imageModelID(ai.Model{ID: "gpt-image-2"}))
}
