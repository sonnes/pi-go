package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"

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
		case ai.File:
			if part, ok := convertFile(v); ok {
				parts = append(parts, part)
			}
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

// convertFile converts an ai.File to an OpenAI Chat Completions file part.
// OpenAI supports inline base64 (FileData), uploaded file IDs (FileID), but
// not arbitrary URLs. Files referenced only by URL are skipped.
func convertFile(f ai.File) (openai.ChatCompletionContentPartUnionParam, bool) {
	if f.FileID == "" && f.Data == "" {
		return openai.ChatCompletionContentPartUnionParam{}, false
	}

	fileParam := openai.ChatCompletionContentPartFileFileParam{}
	if f.FileID != "" {
		fileParam.FileID = param.NewOpt(f.FileID)
	}
	if f.Data != "" {
		dataURL := fmt.Sprintf(
			"data:%s;base64,%s",
			f.MimeType,
			f.Data,
		)
		fileParam.FileData = param.NewOpt(dataURL)
	}
	if f.Filename != "" {
		fileParam.Filename = param.NewOpt(f.Filename)
	}

	return openai.ChatCompletionContentPartUnionParam{
		OfFile: &openai.ChatCompletionContentPartFileParam{
			File: fileParam,
		},
	}, true
}

// convertAssistantMessage converts an assistant message to OpenAI format.
func convertAssistantMessage(msg ai.Message) openai.ChatCompletionMessageParamUnion {
	var text strings.Builder
	var toolCalls []openai.ChatCompletionMessageToolCallUnionParam

	for _, c := range msg.Content {
		switch v := c.(type) {
		case ai.Text:
			text.WriteString(v.Text)
		case ai.Thinking:
			// OpenAI doesn't support a separate thinking field in input messages
			text.WriteString(v.Thinking)
		case ai.ToolCall:
			args, _ := json.Marshal(v.Arguments)
			toolCalls = append(
				toolCalls,
				openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: v.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      v.Name,
							Arguments: string(args),
						},
					},
				},
			)
		}
	}

	assistantMsg := &openai.ChatCompletionAssistantMessageParam{}
	if text.String() != "" {
		assistantMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
			OfString: param.NewOpt(text.String()),
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
	var text strings.Builder
	for _, c := range msg.Content {
		if t, ok := ai.AsContent[ai.Text](c); ok {
			text.WriteString(t.Text)
		}
	}
	return openai.ToolMessage(text.String(), msg.ToolCallID)
}

// convertTools converts ai.ToolInfo definitions to OpenAI tool params.
func convertTools(tools []ai.ToolInfo, compat Compat) []openai.ChatCompletionToolUnionParam {
	result := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
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

		result = append(result, openai.ChatCompletionToolUnionParam{
			OfFunction: &openai.ChatCompletionFunctionToolParam{
				Function: fn,
			},
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
			OfFunctionToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
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

// mapUsage converts OpenAI usage to [ai.Usage], priced with model's rates.
//
// The API reports prompt_tokens inclusive of the cached prefix, so the prefix
// is subtracted out and billed once at the cache-read rate rather than twice.
// Cache writes are implicit — the API reports no token count for them, so
// there is nothing to bill. Reasoning tokens are already counted in
// completion_tokens and bill at the output rate.
func mapUsage(model ai.Model, u openai.CompletionUsage) ai.Usage {
	usage := ai.Usage{
		Input:  int(u.PromptTokens),
		Output: int(u.CompletionTokens),
	}
	if u.PromptTokensDetails.CachedTokens > 0 {
		usage.CacheRead = int(u.PromptTokensDetails.CachedTokens)
		usage.Input -= usage.CacheRead
	}

	rates := model.Cost
	usage.Cost = ai.UsageCost{
		Input:     perMillion(usage.Input, rates.Input),
		Output:    perMillion(usage.Output, rates.Output),
		CacheRead: perMillion(usage.CacheRead, rates.CacheRead),
	}

	return usage
}

// perMillion prices tokens at rate, which is quoted per million tokens.
func perMillion(tokens int, rate float64) float64 {
	return float64(tokens) * rate / 1_000_000
}
