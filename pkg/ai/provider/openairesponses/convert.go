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
		case ai.File:
			if part, ok := convertFile(v); ok {
				parts = append(parts, part)
			}
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

// convertFile converts an ai.File to a Responses API input file part.
// The Responses API supports FileID (uploaded), FileData (base64), and FileURL.
func convertFile(f ai.File) (responses.ResponseInputContentUnionParam, bool) {
	if f.FileID == "" && f.Data == "" && f.URL == "" {
		return responses.ResponseInputContentUnionParam{}, false
	}

	fileParam := &responses.ResponseInputFileParam{}
	if f.FileID != "" {
		fileParam.FileID = param.NewOpt(f.FileID)
	}
	if f.URL != "" {
		fileParam.FileURL = param.NewOpt(f.URL)
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

	return responses.ResponseInputContentUnionParam{
		OfInputFile: fileParam,
	}, true
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
// Function tools become OfFunction; server tools route through convertServerTool
// and are silently skipped if the type is unsupported.
func convertTools(
	tools []ai.ToolInfo,
) []responses.ToolUnionParam {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		if t.Kind == ai.ToolKindServer {
			if p, ok := convertServerTool(t); ok {
				result = append(result, p)
			}
			continue
		}

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

// convertServerTool maps a pi-go server-tool ToolInfo to a Responses API typed
// tool param. Returns false if the type is not currently supported by this
// adapter.
//
// Supported config keys:
//   - web_search: search_context_size ("low"|"medium"|"high"), type
//     ("web_search_preview"|"web_search_preview_2025_03_11")
//   - code_execution: container (string container ID; empty = "auto")
func convertServerTool(t ai.ToolInfo) (responses.ToolUnionParam, bool) {
	switch t.ServerType {
	case ai.ServerToolWebSearch:
		ws := &responses.WebSearchToolParam{
			Type: responses.WebSearchToolTypeWebSearchPreview,
		}
		if v, ok := t.ServerConfig["type"].(string); ok && v != "" {
			ws.Type = responses.WebSearchToolType(v)
		}
		if v, ok := t.ServerConfig["search_context_size"].(string); ok && v != "" {
			ws.SearchContextSize = responses.WebSearchToolSearchContextSize(v)
		}
		return responses.ToolUnionParam{OfWebSearchPreview: ws}, true

	case ai.ServerToolCodeExecution:
		ci := &responses.ToolCodeInterpreterParam{
			Container: responses.ToolCodeInterpreterContainerUnionParam{
				OfCodeInterpreterContainerAuto: &responses.ToolCodeInterpreterContainerCodeInterpreterContainerAutoParam{},
			},
		}
		if v, ok := t.ServerConfig["container"].(string); ok && v != "" {
			ci.Container = responses.ToolCodeInterpreterContainerUnionParam{
				OfString: param.NewOpt(v),
			}
		}
		return responses.ToolUnionParam{OfCodeInterpreter: ci}, true

	default:
		return responses.ToolUnionParam{}, false
	}
}

// openRouterServerToolName maps a pi-go [ai.ServerToolType] to the OpenRouter
// namespaced tool type. Server tools that OpenRouter does not expose return
// the empty string so the caller can drop them silently.
func openRouterServerToolName(t ai.ServerToolType) string {
	switch t {
	case ai.ServerToolWebSearch:
		return "openrouter:web_search"
	case ai.ServerToolWebFetch:
		return "openrouter:web_fetch"
	case ai.ServerToolDateTime:
		return "openrouter:datetime"
	default:
		return ""
	}
}

// convertOpenRouterTools converts pi-go tools to the JSON-shaped slice that
// OpenRouter's Responses API expects in the request body's "tools" array.
//
// Function tools become `{"type": "function", ...}`; server tools become
// `{"type": "openrouter:<name>", ...}` with [ai.ToolInfo.ServerConfig] keys
// merged in. Server-tool types that OpenRouter does not expose (code
// execution, file search, computer, MCP, bash, text editor) are dropped
// silently — same convention as [convertServerTool] for the OpenAI adapter.
func convertOpenRouterTools(tools []ai.ToolInfo) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		if t.Kind == ai.ToolKindServer {
			name := openRouterServerToolName(t.ServerType)
			if name == "" {
				continue
			}
			tool := map[string]any{"type": name}
			for k, v := range t.ServerConfig {
				tool[k] = v
			}
			result = append(result, tool)
			continue
		}

		fn := map[string]any{
			"type":        "function",
			"name":        t.Name,
			"description": t.Description,
			"strict":      false,
		}
		if t.InputSchema != nil {
			if data, err := json.Marshal(t.InputSchema); err == nil {
				var schemaMap map[string]any
				_ = json.Unmarshal(data, &schemaMap)
				fn["parameters"] = schemaMap
			}
		}
		result = append(result, fn)
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
