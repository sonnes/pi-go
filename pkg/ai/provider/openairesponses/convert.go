package openairesponses

import (
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

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
			Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
				OfString: param.NewOpt(text),
			},
		},
	}
}

// filterFunctionTools returns only function-kind tools, dropping server tools.
// Used for the Codex dialect whose backend rejects server tool types such as
// web_search.
func filterFunctionTools(tools []ai.ToolInfo) []ai.ToolInfo {
	out := make([]ai.ToolInfo, 0, len(tools))
	for _, t := range tools {
		if t.Kind == ai.ToolKindFunction {
			out = append(out, t)
		}
	}
	return out
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
//     ("web_search"|"web_search_2025_08_26")
//   - code_execution: container (string container ID; empty = "auto")
//   - file_search: vector_store_ids ([]string, required), max_num_results
//   - computer: no configuration
func convertServerTool(t ai.ToolInfo) (responses.ToolUnionParam, bool) {
	switch t.ServerType {
	case ai.ServerToolWebSearch:
		ws := &responses.WebSearchToolParam{
			Type: responses.WebSearchToolTypeWebSearch,
		}
		if v, ok := t.ServerConfig["type"].(string); ok && v != "" {
			ws.Type = responses.WebSearchToolType(v)
		}
		if v, ok := t.ServerConfig["search_context_size"].(string); ok && v != "" {
			ws.SearchContextSize = responses.WebSearchToolSearchContextSize(v)
		}
		return responses.ToolUnionParam{OfWebSearch: ws}, true

	case ai.ServerToolCodeExecution:
		ci := &responses.ToolCodeInterpreterParam{
			Container: responses.ToolCodeInterpreterContainerUnionParam{
				OfCodeInterpreterToolAuto: &responses.ToolCodeInterpreterContainerCodeInterpreterContainerAutoParam{},
			},
		}
		if v, ok := t.ServerConfig["container"].(string); ok && v != "" {
			ci.Container = responses.ToolCodeInterpreterContainerUnionParam{
				OfString: param.NewOpt(v),
			}
		}
		return responses.ToolUnionParam{OfCodeInterpreter: ci}, true

	case ai.ServerToolFileSearch:
		vectorStoreIDs := serverStringSlice(t.ServerConfig["vector_store_ids"])
		if len(vectorStoreIDs) == 0 {
			return responses.ToolUnionParam{}, false
		}
		fs := &responses.FileSearchToolParam{
			VectorStoreIDs: vectorStoreIDs,
		}
		if n, ok := serverInt64(t.ServerConfig["max_num_results"]); ok {
			fs.MaxNumResults = param.NewOpt(n)
		}
		return responses.ToolUnionParam{OfFileSearch: fs}, true

	case ai.ServerToolComputer:
		return responses.ToolUnionParam{
			OfComputer: &responses.ComputerToolParam{},
		}, true

	default:
		return responses.ToolUnionParam{}, false
	}
}

func serverStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, value := range v {
			text, ok := value.(string)
			if !ok || text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func serverInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		if v == float64(int64(v)) {
			return int64(v), true
		}
	}
	return 0, false
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

// convertInputTokenToolChoice converts a tool choice for the Responses
// input-token endpoint, which uses a distinct SDK union type.
func convertInputTokenToolChoice(
	tc ai.ToolChoice,
) responses.InputTokenCountParamsToolChoiceUnion {
	switch tc {
	case ai.ToolChoiceAuto:
		return responses.InputTokenCountParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsAuto),
		}
	case ai.ToolChoiceNone:
		return responses.InputTokenCountParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsNone),
		}
	case ai.ToolChoiceRequired:
		return responses.InputTokenCountParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsRequired),
		}
	default:
		return responses.InputTokenCountParamsToolChoiceUnion{
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

// mapUsage converts Responses API usage to [ai.Usage], priced with model's
// rates.
//
// The API reports input_tokens inclusive of the cached prefix, so the prefix
// is subtracted out and billed once at the cache-read rate rather than twice.
// Cache writes are implicit — the API reports no token count for them, so
// there is nothing to bill. Reasoning tokens are already counted in
// output_tokens and bill at the output rate.
func mapUsage(model ai.Model, u responses.ResponseUsage) ai.Usage {
	cacheRead := int(u.InputTokensDetails.CachedTokens)

	usage := ai.Usage{
		Input:     int(u.InputTokens) - cacheRead,
		Output:    int(u.OutputTokens),
		CacheRead: cacheRead,
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
