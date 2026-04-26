package anthropic

import (
	"encoding/base64"
	"encoding/json"

	ai "github.com/sonnes/pi-go/pkg/ai"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// convertMessages converts messages to Anthropic MessageParam format.
// Consecutive tool result messages are grouped into a single user message.
func convertMessages(messages []ai.Message) []anthropic.MessageParam {
	var result []anthropic.MessageParam

	i := 0
	for i < len(messages) {
		msg := messages[i]

		switch msg.Role {
		case ai.RoleUser:
			blocks := convertUserContent(msg.Content)
			if len(blocks) > 0 {
				result = append(result, anthropic.NewUserMessage(blocks...))
			}
			i++

		case ai.RoleAssistant:
			blocks := convertAssistantContent(msg.Content)
			if len(blocks) > 0 {
				result = append(result, anthropic.NewAssistantMessage(blocks...))
			}
			i++

		case ai.RoleToolResult:
			// Group consecutive tool result messages into one user message.
			var blocks []anthropic.ContentBlockParamUnion
			for i < len(messages) && messages[i].Role == ai.RoleToolResult {
				blocks = append(blocks, convertToolResult(messages[i]))
				i++
			}
			if len(blocks) > 0 {
				result = append(result, anthropic.NewUserMessage(blocks...))
			}

		default:
			i++
		}
	}

	return result
}

// convertUserContent converts user message content to Anthropic blocks.
func convertUserContent(content []ai.Content) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	for _, c := range content {
		switch v := c.(type) {
		case ai.Text:
			blocks = append(blocks, anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{
					Text: v.Text,
				},
			})
		case ai.Image:
			blocks = append(
				blocks,
				anthropic.NewImageBlockBase64(v.MimeType, v.Data),
			)
		case ai.File:
			if block, ok := convertFile(v); ok {
				blocks = append(blocks, block)
			}
		}
	}

	return blocks
}

// convertFile converts an ai.File into an Anthropic document block. Anthropic
// supports PDF (base64 or URL) and plain-text documents. Unsupported MIME
// types or files referencing a provider FileID are skipped.
func convertFile(f ai.File) (anthropic.ContentBlockParamUnion, bool) {
	switch {
	case f.URL != "":
		return anthropic.NewDocumentBlock(anthropic.URLPDFSourceParam{
			URL: f.URL,
		}), true
	case f.Data != "" && f.MimeType == "text/plain":
		// Per ai.File contract, Data is base64-encoded. Anthropic's
		// plain-text document source expects raw text, so decode here.
		raw, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return anthropic.ContentBlockParamUnion{}, false
		}
		return anthropic.NewDocumentBlock(anthropic.PlainTextSourceParam{
			Data: string(raw),
		}), true
	case f.Data != "" && f.MimeType == "application/pdf":
		return anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
			Data: f.Data,
		}), true
	default:
		return anthropic.ContentBlockParamUnion{}, false
	}
}

// convertAssistantContent converts assistant message content to Anthropic blocks.
func convertAssistantContent(content []ai.Content) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	for _, c := range content {
		switch v := c.(type) {
		case ai.Text:
			blocks = append(blocks, anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{
					Text: v.Text,
				},
			})
		case ai.Thinking:
			if v.Signature != "" {
				blocks = append(
					blocks,
					anthropic.NewThinkingBlock(v.Signature, v.Thinking),
				)
			}
		case ai.ToolCall:
			blocks = append(
				blocks,
				anthropic.NewToolUseBlock(v.ID, v.Arguments, v.Name),
			)
		}
	}

	return blocks
}

// convertToolResult converts a tool result message to an Anthropic tool result block.
func convertToolResult(msg ai.Message) anthropic.ContentBlockParamUnion {
	toolResultBlock := anthropic.ToolResultBlockParam{
		ToolUseID: msg.ToolCallID,
	}

	var textParts []anthropic.ToolResultBlockParamContentUnion
	for _, c := range msg.Content {
		if t, ok := c.(ai.Text); ok {
			textParts = append(textParts, anthropic.ToolResultBlockParamContentUnion{
				OfText: &anthropic.TextBlockParam{
					Text: t.Text,
				},
			})
		}
	}
	toolResultBlock.Content = textParts

	if msg.IsError {
		toolResultBlock.IsError = param.NewOpt(true)
	}

	return anthropic.ContentBlockParamUnion{
		OfToolResult: &toolResultBlock,
	}
}

// convertTools converts ai.ToolInfo definitions to Anthropic ToolUnionParam format.
func convertTools(tools []ai.ToolInfo) []anthropic.ToolUnionParam {
	result := make([]anthropic.ToolUnionParam, 0, len(tools))

	for _, t := range tools {
		var properties any
		var required []string

		if t.InputSchema != nil {
			if data, err := json.Marshal(t.InputSchema); err == nil {
				var schema map[string]any
				if err := json.Unmarshal(data, &schema); err == nil {
					if p, ok := schema["properties"]; ok {
						properties = p
					}
					if r, ok := schema["required"]; ok {
						if reqArr, ok := r.([]any); ok {
							for _, v := range reqArr {
								if s, ok := v.(string); ok {
									required = append(required, s)
								}
							}
						}
					}
				}
			}
		}

		result = append(result, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: properties,
					Required:   required,
				},
			},
		})
	}

	return result
}

// convertToolChoice converts a ToolChoice to Anthropic format.
func convertToolChoice(tc ai.ToolChoice) anthropic.ToolChoiceUnionParam {
	switch tc {
	case ai.ToolChoiceAuto:
		return anthropic.ToolChoiceUnionParam{
			OfAuto: &anthropic.ToolChoiceAutoParam{},
		}
	case ai.ToolChoiceRequired:
		return anthropic.ToolChoiceUnionParam{
			OfAny: &anthropic.ToolChoiceAnyParam{},
		}
	case ai.ToolChoiceNone:
		return anthropic.ToolChoiceUnionParam{
			OfNone: &anthropic.ToolChoiceNoneParam{},
		}
	default:
		return anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: string(tc),
			},
		}
	}
}
