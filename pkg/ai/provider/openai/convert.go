package openai

import (
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// convertMessages converts types to OpenAI message params.
func convertMessages(
	system string,
	messages []ai.Message,
	compat Compat,
) []openai.ChatCompletionMessageParamUnion {
	result := make(
		[]openai.ChatCompletionMessageParamUnion,
		0,
		len(messages)+1,
	)

	if system != "" {
		if compat.SupportsDeveloperRole {
			result = append(result, openai.DeveloperMessage(system))
		} else {
			result = append(result, openai.SystemMessage(system))
		}
	}

	for _, msg := range messages {
		switch msg.Role {
		case ai.RoleUser:
			result = append(result, convertUserMessage(msg)...)

		case ai.RoleAssistant:
			result = append(result, convertAssistantMessage(msg))

		case ai.RoleToolResult:
			result = append(result, convertToolResultMessage(msg))
		}
	}

	return result
}

// convertUserMessage converts a user message to OpenAI format.
func convertUserMessage(msg ai.Message) []openai.ChatCompletionMessageParamUnion {
	if len(msg.Content) == 1 {
		if t, ok := ai.AsContent[ai.Text](msg.Content[0]); ok {
			return []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage(t.Text),
			}
		}
	}

	parts := make(
		[]openai.ChatCompletionContentPartUnionParam,
		0,
		len(msg.Content),
	)
	for _, c := range msg.Content {
		switch v := c.(type) {
		case ai.Text:
			parts = append(parts, openai.ChatCompletionContentPartUnionParam{
				OfText: &openai.ChatCompletionContentPartTextParam{
					Text: v.Text,
				},
			})
		case ai.Image:
			dataURL := fmt.Sprintf(
				"data:%s;base64,%s",
				v.MimeType,
				v.Data,
			)
			parts = append(parts, openai.ChatCompletionContentPartUnionParam{
				OfImageURL: &openai.ChatCompletionContentPartImageParam{
					ImageURL: openai.ChatCompletionContentPartImageImageURLParam{
						URL: dataURL,
					},
				},
			})
		}
	}

	return []openai.ChatCompletionMessageParamUnion{
		{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfArrayOfContentParts: parts,
				},
			},
		},
	}
}

// convertAssistantMessage converts an assistant message to OpenAI format.
func convertAssistantMessage(msg ai.Message) openai.ChatCompletionMessageParamUnion {
	var text string
	var toolCalls []openai.ChatCompletionMessageToolCallParam

	for _, c := range msg.Content {
		switch v := c.(type) {
		case ai.Text:
			text += v.Text
		case ai.Thinking:
			// OpenAI doesn't support a separate thinking field in input messages
			text += v.Thinking
		case ai.ToolCall:
			args, _ := json.Marshal(v.Arguments)
			toolCalls = append(
				toolCalls,
				openai.ChatCompletionMessageToolCallParam{
					ID:   v.ID,
					Type: "function",
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      v.Name,
						Arguments: string(args),
					},
				},
			)
		}
	}

	assistantMsg := &openai.ChatCompletionAssistantMessageParam{}
	if text != "" {
		assistantMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
			OfString: param.NewOpt(text),
		}
	}
	if len(toolCalls) > 0 {
		assistantMsg.ToolCalls = toolCalls
	}

	return openai.ChatCompletionMessageParamUnion{
		OfAssistant: assistantMsg,
	}
}

// convertToolResultMessage converts a tool result message to OpenAI format.
func convertToolResultMessage(msg ai.Message) openai.ChatCompletionMessageParamUnion {
	var text string
	for _, c := range msg.Content {
		if t, ok := ai.AsContent[ai.Text](c); ok {
			text += t.Text
		}
	}
	return openai.ToolMessage(msg.ToolCallID, text)
}

// convertTools converts ai.ToolInfo definitions to OpenAI tool params.
func convertTools(tools []ai.ToolInfo, compat Compat) []openai.ChatCompletionToolParam {
	result := make([]openai.ChatCompletionToolParam, 0, len(tools))
	for _, t := range tools {
		var schemaMap map[string]any
		if t.InputSchema != nil {
			if data, err := json.Marshal(t.InputSchema); err == nil {
				_ = json.Unmarshal(data, &schemaMap)
			}
		}

		fn := shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: param.NewOpt(t.Description),
			Parameters:  openai.FunctionParameters(schemaMap),
		}
		if compat.SupportsStrictMode {
			fn.Strict = param.NewOpt(false)
		}

		result = append(result, openai.ChatCompletionToolParam{
			Function: fn,
		})
	}
	return result
}

// convertToolChoice converts ToolChoice to OpenAI format.
func convertToolChoice(
	tc ai.ToolChoice,
) openai.ChatCompletionToolChoiceOptionUnionParam {
	switch tc {
	case ai.ToolChoiceAuto:
		return openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("auto"),
		}
	case ai.ToolChoiceNone:
		return openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("none"),
		}
	case ai.ToolChoiceRequired:
		return openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("required"),
		}
	default:
		return openai.ChatCompletionToolChoiceOptionUnionParam{
			OfChatCompletionNamedToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
				Function: openai.ChatCompletionNamedToolChoiceFunctionParam{
					Name: string(tc),
				},
			},
		}
	}
}

// mapStopReason converts OpenAI finish reason to StopReason.
func mapStopReason(reason string) ai.StopReason {
	switch reason {
	case "stop":
		return ai.StopReasonStop
	case "length":
		return ai.StopReasonLength
	case "tool_calls":
		return ai.StopReasonToolUse
	case "content_filter":
		return ai.StopReasonError
	default:
		return ai.StopReasonStop
	}
}

// mapUsage converts OpenAI usage to Usage.
func mapUsage(u openai.CompletionUsage) ai.Usage {
	usage := ai.Usage{
		Input:  int(u.PromptTokens),
		Output: int(u.CompletionTokens),
		Total:  int(u.TotalTokens),
	}
	if u.PromptTokensDetails.CachedTokens > 0 {
		usage.CacheRead = int(u.PromptTokensDetails.CachedTokens)
	}
	return usage
}
