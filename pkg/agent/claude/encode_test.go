package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeUserContent_SingleText(t *testing.T) {
	msg := ai.UserMessage("hello")

	raw, err := encodeUserContent(msg)
	require.NoError(t, err)

	// Single text → bare string.
	var s string
	require.NoError(t, json.Unmarshal(raw, &s))
	assert.Equal(t, "hello", s)
}

func TestEncodeUserContent_TextAndImage(t *testing.T) {
	msg := ai.UserImageMessage("look at this",
		ai.Image{Data: "AAA=", MimeType: "image/jpeg"},
	)

	raw, err := encodeUserContent(msg)
	require.NoError(t, err)

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal(raw, &blocks))
	require.Len(t, blocks, 2)

	assert.Equal(t, "text", blocks[0]["type"])
	assert.Equal(t, "look at this", blocks[0]["text"])

	assert.Equal(t, "image", blocks[1]["type"])
	src, ok := blocks[1]["source"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "base64", src["type"])
	assert.Equal(t, "image/jpeg", src["media_type"])
	assert.Equal(t, "AAA=", src["data"])
}

func TestEncodeUserContent_ImageDefaultMime(t *testing.T) {
	msg := ai.Message{
		Role:    ai.RoleUser,
		Content: []ai.Content{ai.Text{Text: "x"}, ai.Image{Data: "AAA="}},
	}

	raw, err := encodeUserContent(msg)
	require.NoError(t, err)

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal(raw, &blocks))
	src := blocks[1]["source"].(map[string]any)
	assert.Equal(t, "image/png", src["media_type"])
}

func TestEncodeUserContent_SkipsNonUserBlocks(t *testing.T) {
	msg := ai.Message{
		Role: ai.RoleUser,
		Content: []ai.Content{
			ai.Text{Text: "hi"},
			ai.Thinking{Thinking: "ignored"},
			ai.ToolCall{ID: "t1", Name: "Read"},
		},
	}

	raw, err := encodeUserContent(msg)
	require.NoError(t, err)

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal(raw, &blocks))
	require.Len(t, blocks, 1)
	assert.Equal(t, "text", blocks[0]["type"])
}

func TestEncodeUserContent_EmptyContentErrors(t *testing.T) {
	_, err := encodeUserContent(ai.Message{Role: ai.RoleUser})
	require.Error(t, err)
}

func TestEncodeUserContent_OnlyNonUserBlocksErrors(t *testing.T) {
	msg := ai.Message{
		Role:    ai.RoleUser,
		Content: []ai.Content{ai.Thinking{Thinking: "x"}},
	}
	_, err := encodeUserContent(msg)
	require.Error(t, err)
}

func TestBuildUserLine_Shape(t *testing.T) {
	line, err := buildUserLine(ai.UserMessage("ping"))
	require.NoError(t, err)

	assert.True(t, strings.HasSuffix(string(line), "\n"),
		"line must end with newline for NDJSON framing")

	var decoded sdkUserMessage
	require.NoError(t, json.Unmarshal(line[:len(line)-1], &decoded))

	assert.Equal(t, "user", decoded.Type)
	assert.Equal(t, "user", decoded.Message.Role)
	assert.Equal(t, "", decoded.SessionID)
	assert.Nil(t, decoded.ParentToolUseID)

	var content string
	require.NoError(t, json.Unmarshal(decoded.Message.Content, &content))
	assert.Equal(t, "ping", content)
}

func TestBuildUserLine_ParentToolUseIDIsNull(t *testing.T) {
	line, err := buildUserLine(ai.UserMessage("ping"))
	require.NoError(t, err)

	// The JSON must have an explicit null, not an omitted field — the
	// TypeScript SDK checks for `null` specifically.
	assert.Contains(t, string(line), `"parent_tool_use_id":null`)
}
