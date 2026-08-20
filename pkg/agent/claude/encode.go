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

// encodeUserContent serializes the content blocks of the user messages
// of one turn into the `content` field of a single SDKUserMessage.
//
// Every user line on stdin starts a turn of its own in the CLI, so the
// whole turn has to arrive as one line. encodeUserContent therefore
// concatenates the blocks of every user message in the order given.
// Injected context that the caller wrote before the message of the user
// keeps its position ahead of it.
//
// One [ai.Text] block becomes a bare JSON string. Anything richer
// becomes an array of Anthropic content blocks built from
// [anthropic.ContentBlockParamUnion]. Richer content is several text
// blocks, images, or a mix of both. Non-user block types, such as
// [ai.Thinking] and [ai.ToolCall], are not valid on a user turn, and the
// function removes them. It also skips a message that is not from the
// user: the CLI keeps its own transcript, so replaying an assistant
// message here is not this package's business.
func encodeUserContent(msgs []ai.Message) (json.RawMessage, error) {
	var blocks []anthropic.ContentBlockParamUnion
	for _, msg := range msgs {
		if msg.Role != ai.RoleUser {
			continue
		}
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
	}

	if len(blocks) == 0 {
		return nil, errors.New(
			"claude: turn has no user text or image content to send",
		)
	}

	// The common turn is one line of text. Keep the compact wire shape
	// for it.
	if len(blocks) == 1 && blocks[0].OfText != nil {
		return json.Marshal(blocks[0].OfText.Text)
	}

	return json.Marshal(blocks)
}

// buildUserLine returns one NDJSON byte slice with a trailing newline.
// The slice is the single SDKUserMessage that carries the whole turn.
func buildUserLine(msgs []ai.Message) ([]byte, error) {
	content, err := encodeUserContent(msgs)
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
