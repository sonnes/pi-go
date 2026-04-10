package openairesponses

import (
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// convertInput converts ai messages to Responses API input items.
// System prompt is handled separately via params.Instructions.
func convertInput(
	messages []ai.Message,
) responses.ResponseInputParam {
	var items responses.ResponseInputParam

	for _, msg := range messages {
		switch msg.Role {
		case ai.RoleUser:
			items = append(items, convertUserMessage(msg)...)
		case ai.RoleAssistant:
			items = append(items, convertAssistantMessage(msg)...)
		case ai.RoleToolResult:
			items = append(items, convertToolResultMessage(msg))
		}
	}

	return items
}

// convertUserMessage converts a user message to Responses API input items.
func convertUserMessage(
	msg ai.Message,
) []responses.ResponseInputItemUnionParam {
	if len(msg.Content) == 1 {
		if t, ok := ai.AsContent[ai.Text](msg.Content[0]); ok {
			return []responses.ResponseInputItemUnionParam{
				{
					OfMessage: &responses.EasyInputMessageParam{
						Role: responses.EasyInputMessageRoleUser,
						Content: responses.EasyInputMessageContentUnionParam{
							OfString: param.NewOpt(t.Text),
						},
					},
				},
			}
		}
	}

	var parts responses.ResponseInputMessageContentListParam
	for _, c := range msg.Content {
		switch v := c.(type) {
		case ai.Text:
			parts = append(
				parts,
				responses.ResponseInputContentUnionParam{
					OfInputText: &responses.ResponseInputTextParam{
						Text: v.Text,
					},
				},
			)
		case ai.Image:
			dataURL := fmt.Sprintf(
				"data:%s;base64,%s",
				v.MimeType,
				v.Data,
			)
			parts = append(
				parts,
				responses.ResponseInputContentUnionParam{
					OfInputImage: &responses.ResponseInputImageParam{
						ImageURL: param.NewOpt(dataURL),
						Detail:   responses.ResponseInputImageDetailAuto,
					},
				},
			)
		}
	}

	return []responses.ResponseInputItemUnionParam{
		{
			OfMessage: &responses.EasyInputMessageParam{
				Role: responses.EasyInputMessageRoleUser,
				Content: responses.EasyInputMessageContentUnionParam{
					OfInputItemContentList: parts,
				},
			},
		},
	}
}

// convertAssistantMessage converts an assistant message to Responses API
// input items. Unlike chat completions, tool calls and reasoning are
// separate input items, not part of the assistant message.
func convertAssistantMessage(
	msg ai.Message,
) []responses.ResponseInputItemUnionParam {
	var items []responses.ResponseInputItemUnionParam
	var text string

	for _, c := range msg.Content {
		switch v := c.(type) {
		case ai.Text:
			text += v.Text

		case ai.Thinking:
			if v.Signature != "" {
				items = append(items, responses.ResponseInputItemUnionParam{
					OfReasoning: &responses.ResponseReasoningItemParam{
						ID: v.Signature,
						Summary: []responses.ResponseReasoningItemSummaryParam{
							{Text: v.Thinking},
						},
					},
				})
			}

		case ai.ToolCall:
			args, _ := json.Marshal(v.Arguments)
			item := responses.ResponseInputItemUnionParam{
				OfFunctionCall: &responses.ResponseFunctionToolCallParam{
					CallID:    v.ID,
					Name:      v.Name,
					Arguments: string(args),
				},
			}
			if v.ID != "" {
				item.OfFunctionCall.ID = param.NewOpt(v.ID)
			}
			items = append(items, item)
		}
	}

	if text != "" {
		items = append([]responses.ResponseInputItemUnionParam{
			{
				OfMessage: &responses.EasyInputMessageParam{
					Role: responses.EasyInputMessageRoleAssistant,
					Content: responses.EasyInputMessageContentUnionParam{
						OfString: param.NewOpt(text),
					},
				},
			},
		}, items...)
	}

	return items
}

// convertToolResultMessage converts a tool result to a function call output.
func convertToolResultMessage(
	msg ai.Message,
) responses.ResponseInputItemUnionParam {
	var text string
	for _, c := range msg.Content {
		if t, ok := ai.AsContent[ai.Text](c); ok {
			text += t.Text
		}
	}

	return responses.ResponseInputItemUnionParam{
		OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
			CallID: msg.ToolCallID,
			Output: text,
		},
	}
}

// convertTools converts ai.ToolInfo to Responses API tool params.
func convertTools(
	tools []ai.ToolInfo,
) []responses.ToolUnionParam {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		var schemaMap map[string]any
		if t.InputSchema != nil {
			if data, err := json.Marshal(t.InputSchema); err == nil {
				_ = json.Unmarshal(data, &schemaMap)
			}
		}

		result = append(result, responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        t.Name,
				Description: param.NewOpt(t.Description),
				Parameters:  schemaMap,
				Strict:      param.NewOpt(false),
			},
		})
	}
	return result
}

// convertToolChoice converts ai.ToolChoice to Responses API format.
func convertToolChoice(
	tc ai.ToolChoice,
) responses.ResponseNewParamsToolChoiceUnion {
	switch tc {
	case ai.ToolChoiceAuto:
		return responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(
				responses.ToolChoiceOptionsAuto,
			),
		}
	case ai.ToolChoiceNone:
		return responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(
				responses.ToolChoiceOptionsNone,
			),
		}
	case ai.ToolChoiceRequired:
		return responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(
				responses.ToolChoiceOptionsRequired,
			),
		}
	default:
		return responses.ResponseNewParamsToolChoiceUnion{
			OfFunctionTool: &responses.ToolChoiceFunctionParam{
				Name: string(tc),
			},
		}
	}
}

// mapStopReason converts Responses API status to ai.StopReason.
func mapStopReason(status responses.ResponseStatus) ai.StopReason {
	switch status {
	case responses.ResponseStatusCompleted:
		return ai.StopReasonStop
	case responses.ResponseStatusIncomplete:
		return ai.StopReasonLength
	case responses.ResponseStatusFailed, responses.ResponseStatusCancelled:
		return ai.StopReasonError
	default:
		return ai.StopReasonStop
	}
}

// mapUsage converts Responses API usage to ai.Usage.
func mapUsage(u responses.ResponseUsage) ai.Usage {
	return ai.Usage{
		Input:     int(u.InputTokens),
		Output:    int(u.OutputTokens),
		Total:     int(u.TotalTokens),
		CacheRead: int(u.InputTokensDetails.CachedTokens),
	}
}

// mapThinkingLevel converts ai.ThinkingLevel to OpenAI reasoning effort.
func mapThinkingLevel(level ai.ThinkingLevel) shared.ReasoningEffort {
	switch level {
	case ai.ThinkingLow:
		return shared.ReasoningEffortLow
	case ai.ThinkingMedium:
		return shared.ReasoningEffortMedium
	case ai.ThinkingHigh, ai.ThinkingXHigh:
		return shared.ReasoningEffortHigh
	default:
		return shared.ReasoningEffortMedium
	}
}
