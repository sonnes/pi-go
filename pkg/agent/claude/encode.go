package claude

import (
	"encoding/json"
	"errors"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/sonnes/pi-go/pkg/ai"
)

// sdkUserMessage is the stdin wire format that `claude --print
// --input-format stream-json` reads. It matches the SDKUserMessage type
// in the Claude CLI (see cc/server/directConnectManager.ts).
type sdkUserMessage struct {
	Type            string              `json:"type"`
	Message         sdkUserMessageInner `json:"message"`
	ParentToolUseID *string             `json:"parent_tool_use_id"`
	SessionID       string              `json:"session_id"`
}

type sdkUserMessageInner struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// encodeUserContent serializes the content blocks of a user message into
// the `content` field of an SDKUserMessage.
//
// One [ai.Text] block becomes a bare JSON string. Richer content becomes
// an array of Anthropic content blocks built from
// [anthropic.ContentBlockParamUnion]. Richer content is multiple text
// blocks, images, or a mix of both. Non-user block types, such as
// [ai.Thinking] and [ai.ToolCall], are not valid on a user turn. The
// function removes them.
func encodeUserContent(msg ai.Message) (json.RawMessage, error) {
	if len(msg.Content) == 0 {
		return nil, errors.New("claude: user message has no content")
	}

	if len(msg.Content) == 1 {
		if t, ok := ai.AsContent[ai.Text](msg.Content[0]); ok {
			return json.Marshal(t.Text)
		}
	}

	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content))
	for _, c := range msg.Content {
		switch v := c.(type) {
		case ai.Text:
			blocks = append(blocks, anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{Text: v.Text},
			})
		case ai.Image:
			mime := v.MimeType
			if mime == "" {
				mime = "image/png"
			}
			blocks = append(blocks, anthropic.NewImageBlockBase64(mime, v.Data))
		default:
			// Skip Thinking, ToolCall, and any future types.
		}
	}

	if len(blocks) == 0 {
		return nil, errors.New("claude: user message has no text or image content")
	}

	return json.Marshal(blocks)
}

// buildUserLine returns one NDJSON byte slice with a trailing newline.
// The slice is the SDKUserMessage for the given user message.
func buildUserLine(msg ai.Message) ([]byte, error) {
	content, err := encodeUserContent(msg)
	if err != nil {
		return nil, err
	}

	line := sdkUserMessage{
		Type: "user",
		Message: sdkUserMessageInner{
			Role:    "user",
			Content: content,
		},
		ParentToolUseID: nil,
		SessionID:       "",
	}

	b, err := json.Marshal(line)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
