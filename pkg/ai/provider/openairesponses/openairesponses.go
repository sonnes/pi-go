// Package openairesponses provides an OpenAI Responses API provider.
package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/tidwall/gjson"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// Dialect selects the request and response shape of a backend. Every
// backend shares the OpenAI Responses API surface. The backends differ in
// tool names and in SSE event types.
type Dialect int

const (
	// DialectOpenAI is the native OpenAI Responses API. It is the default.
	DialectOpenAI Dialect = iota
	// DialectOpenRouter targets the `/v1/responses` endpoint of OpenRouter.
	// Server tools use the `openrouter:*` namespace. Text deltas arrive on
	// `response.content_part.delta` instead of `response.output_text.delta`.
	// Reasoning can use `response.reasoning.delta` or
	// `response.reasoning_text.delta` instead of reasoning-summary events.
	// See [docs/plans/2026-04-28-openrouter-responses-dialect.md] for the
	// full list of differences that this dialect compensates for.
	DialectOpenRouter
	// DialectCodex targets the ChatGPT Codex backend at
	// `chatgpt.com/backend-api/codex/responses`. The endpoint accepts the
	// same Responses API SSE format as OpenAI. It adds one request-shape
	// requirement. The `instructions` field must always be set and must not
	// be empty, even when the caller gives no system prompt. If the field
	// is empty, the backend returns `{"detail":"Instructions are required"}`.
	DialectCodex
)

// codexDefaultInstructions is the fallback `instructions` value that
// [DialectCodex] sends when the caller gives no system prompt. The Codex
// backend rejects a request with an empty instructions field. The exact
// text does not matter. Only a non-empty value matters.
const codexDefaultInstructions = "You are a helpful coding assistant."

// Provider implements [ai.TextProvider] for the OpenAI Responses API.
//
// One instance can target OpenAI. Another instance can target OpenRouter
// through [DialectOpenRouter]. An application registers each instance under
// the catalog identity that it needs.
type Provider struct {
	client  *openai.Client
	dialect Dialect
}

// Compile-time interface checks.
var (
	_ ai.TextProvider   = (*Provider)(nil)
	_ ai.ObjectProvider = (*Provider)(nil)
	_ ai.TokenCounter   = (*Provider)(nil)
)

// ID is the OpenAI Responses provider identity.
const ID = "openai-responses"

// New creates an OpenAI Responses provider for the native OpenAI API.
// For OpenRouter, use [NewForOpenRouter].
func New(opts ...option.RequestOption) *Provider {
	opts = append(
		[]option.RequestOption{option.WithMaxRetries(0)},
		opts...,
	)
	client := openai.NewClient(opts...)
	return &Provider{client: &client, dialect: DialectOpenAI}
}

// NewForOpenRouter creates a Responses API provider for OpenRouter. The
// caller must pass [option.WithBaseURL]("https://openrouter.ai/api/v1") and
// [option.WithAPIKey] with the OpenRouter key.
//
// The provider translates server tools to the `openrouter:*` namespace. It
// maps the SSE events from `/v1/responses` onto the standard pi-go event
// stream. See [Dialect] for the full list of differences.
func NewForOpenRouter(opts ...option.RequestOption) *Provider {
	p := New(opts...)
	p.dialect = DialectOpenRouter
	return p
}

// NewForCodex creates a Responses API provider for the ChatGPT Codex
// backend. The caller must set the base URL to
// "https://chatgpt.com/backend-api/codex". The caller must also supply a
// ChatGPT OAuth token, for example through the OAuth transport. Only the
// request-shape adjustments live here. See [DialectCodex] for the
// differences that this dialect compensates for.
func NewForCodex(opts ...option.RequestOption) *Provider {
	p := New(opts...)
	p.dialect = DialectCodex
	return p
}

// DetectOpenRouter builds an OpenRouter-dialect provider from
// OPENROUTER_API_KEY and reports whether that variable is set.
func DetectOpenRouter() (*Provider, bool) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, false
	}
	return NewForOpenRouter(
		option.WithAPIKey(key),
		option.WithBaseURL("https://openrouter.ai/api/v1"),
	), true
}

// StreamText streams a text response through the Responses API.
func (p *Provider) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) *ai.EventStream {
	log.Debug(
		"[OPENAI-RESPONSES] starting stream",
		"model", model.ID,
		"messages", len(prompt.Messages),
		"tools", len(prompt.Tools),
	)

	params := buildParams(model, prompt, opts, p.dialect)
	reqOpts := mergeHeaders(model.Headers, opts.Headers)

	// For the OpenRouter dialect, replace the "tools" field of the request
	// body with the openrouter:* server-tool shapes. The typed
	// ToolUnionParam of the OpenAI SDK cannot express them. See
	// convertOpenRouterTools.
	if p.dialect == DialectOpenRouter && len(prompt.Tools) > 0 {
		orTools := convertOpenRouterTools(prompt.Tools)
		if len(orTools) > 0 {
			reqOpts = append(reqOpts, option.WithJSONSet("tools", orTools))
		}
	}

	// Map ServerToolType to the canonical Name that the caller registered,
	// for example ServerToolWebSearch to "WebSearch". When the provider
	// returns a server-tool result, this map sets ToolCall.Name to the
	// name that function tools use. Without it, the name is the raw
	// provider item type ("web_search_call", "openrouter:web_search").
	serverToolNames := serverToolNameByType(prompt.Tools)

	return ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		stream := p.client.Responses.NewStreaming(
			ctx,
			*params,
			reqOpts...,
		)

		var (
			contentIndex    int
			inText          bool
			inThink         bool
			toolCalls       = make(map[int64]*streamToolCall)
			serverToolCalls []ai.ToolCall
			usage           ai.Usage
			stopReason      ai.StopReason
			textAccum       strings.Builder
			thinkAccum      strings.Builder
		)

		for stream.Next() {
			event := stream.Current()

			switch event.Type {
			case "response.reasoning_summary_text.delta",
				"response.reasoning_text.delta",
				"response.reasoning.delta":
				delta := event.Delta
				if !inThink {
					inThink = true
					push(ai.Event{
						Type:         ai.EventThinkStart,
						ContentIndex: contentIndex,
					})
				}
				thinkAccum.WriteString(delta)
				push(ai.Event{
					Type:         ai.EventThinkDelta,
					ContentIndex: contentIndex,
					Delta:        delta,
				})

			case "response.reasoning_summary_part.done",
				"response.reasoning_text.done",
				"response.reasoning.done":
				if inThink {
					inThink = false
					push(ai.Event{
						Type:         ai.EventThinkEnd,
						ContentIndex: contentIndex,
						Content:      thinkAccum.String(),
					})
					contentIndex++
				}

			case "response.output_text.delta":
				delta := event.Delta
				if inThink {
					inThink = false
					push(ai.Event{
						Type:         ai.EventThinkEnd,
						ContentIndex: contentIndex,
						Content:      thinkAccum.String(),
					})
					contentIndex++
				}
				if !inText {
					inText = true
					push(ai.Event{
						Type:         ai.EventTextStart,
						ContentIndex: contentIndex,
					})
				}
				textAccum.WriteString(delta)
				push(ai.Event{
					Type:         ai.EventTextDelta,
					ContentIndex: contentIndex,
					Delta:        delta,
				})

			case "response.output_text.done":
				if inText {
					inText = false
					push(ai.Event{
						Type:         ai.EventTextEnd,
						ContentIndex: contentIndex,
						Content:      textAccum.String(),
					})
					contentIndex++
				}

			// OpenRouter dialect: text deltas arrive on content_part events
			// and not on output_text events. The typed union of the SDK
			// does not know about content_part.delta. The flat fields
			// still fill from the JSON payload.
			case "response.content_part.delta":
				delta := event.Delta
				if delta == "" {
					// Some OpenRouter payloads put the text under part.text
					// for the first chunk. Use that field instead.
					delta = event.Part.Text
				}
				if delta == "" {
					break
				}
				if !inText {
					inText = true
					push(ai.Event{
						Type:         ai.EventTextStart,
						ContentIndex: contentIndex,
					})
				}
				textAccum.WriteString(delta)
				push(ai.Event{
					Type:         ai.EventTextDelta,
					ContentIndex: contentIndex,
					Delta:        delta,
				})

			case "response.content_part.done":
				// Idempotent close. OpenAI sends this event after
				// output_text.done, so inText is already false. OpenRouter
				// sends it as the real end of the text block.
				if inText {
					inText = false
					push(ai.Event{
						Type:         ai.EventTextEnd,
						ContentIndex: contentIndex,
						Content:      textAccum.String(),
					})
					contentIndex++
				}

			case "response.output_item.added":
				switch event.Item.Type {
				case "function_call":
					if inText {
						inText = false
						push(ai.Event{
							Type:         ai.EventTextEnd,
							ContentIndex: contentIndex,
							Content:      textAccum.String(),
						})
						contentIndex++
					}
					toolCalls[event.OutputIndex] = &streamToolCall{
						id:     event.Item.CallID,
						name:   event.Item.Name,
						itemID: event.Item.ID,
					}
					push(ai.Event{
						Type:         ai.EventToolStart,
						ContentIndex: contentIndex,
						ToolCall: &ai.ToolCall{
							ID:   event.Item.CallID,
							Name: event.Item.Name,
						},
					})
				case "web_search_call", "code_interpreter_call", "file_search_call", "computer_call", "mcp_call":
					if inText {
						inText = false
						push(ai.Event{
							Type:         ai.EventTextEnd,
							ContentIndex: contentIndex,
							Content:      textAccum.String(),
						})
						contentIndex++
					}
					st := serverTypeForItem(event.Item.Type)
					push(ai.Event{
						Type:         ai.EventToolStart,
						ContentIndex: contentIndex,
						ToolCall: &ai.ToolCall{
							ID:         event.Item.ID,
							Name:       canonicalServerToolName(st, serverToolNames, event.Item.Type),
							Server:     true,
							ServerType: st,
						},
					})
				default:
					// OpenRouter dialect: server-tool items use the
					// `openrouter:*` namespace (web_search / web_fetch /
					// datetime / image_generation).
					if strings.HasPrefix(event.Item.Type, "openrouter:") {
						if inText {
							inText = false
							push(ai.Event{
								Type:         ai.EventTextEnd,
								ContentIndex: contentIndex,
								Content:      textAccum.String(),
							})
							contentIndex++
						}
						st := serverTypeForOpenRouterItem(event.Item.Type)
						push(ai.Event{
							Type:         ai.EventToolStart,
							ContentIndex: contentIndex,
							ToolCall: &ai.ToolCall{
								ID:         event.Item.ID,
								Name:       canonicalServerToolName(st, serverToolNames, event.Item.Type),
								Server:     true,
								ServerType: st,
							},
						})
					}
				}

			case "response.output_item.done":
				switch event.Item.Type {
				case "web_search_call", "code_interpreter_call", "file_search_call", "computer_call", "mcp_call":
					call := buildServerToolCall(event.Item)
					call.Name = canonicalServerToolName(call.ServerType, serverToolNames, event.Item.Type)
					serverToolCalls = append(serverToolCalls, call)
					push(ai.Event{
						Type:         ai.EventToolEnd,
						ContentIndex: contentIndex,
						ToolCall:     &call,
					})
					contentIndex++
				default:
					if strings.HasPrefix(event.Item.Type, "openrouter:") {
						call := buildOpenRouterServerToolCall(event.Item)
						call.Name = canonicalServerToolName(call.ServerType, serverToolNames, event.Item.Type)
						serverToolCalls = append(serverToolCalls, call)
						push(ai.Event{
							Type:         ai.EventToolEnd,
							ContentIndex: contentIndex,
							ToolCall:     &call,
						})
						contentIndex++
					}
				}

			case "response.function_call_arguments.delta":
				delta := event.Delta
				tc, ok := toolCalls[event.OutputIndex]
				if ok {
					tc.arguments.WriteString(delta)
					push(ai.Event{
						Type:         ai.EventToolDelta,
						ContentIndex: contentIndex,
						Delta:        delta,
					})
				}

			case "response.function_call_arguments.done":
				tc, ok := toolCalls[event.OutputIndex]
				if ok {
					var args map[string]any
					if tc.arguments.Len() > 0 {
						_ = json.Unmarshal(
							[]byte(tc.arguments.String()),
							&args,
						)
					}
					push(ai.Event{
						Type:         ai.EventToolEnd,
						ContentIndex: contentIndex,
						ToolCall: &ai.ToolCall{
							ID:        tc.id,
							Name:      tc.name,
							Arguments: args,
						},
					})
					contentIndex++
				}

			case "response.completed":
				resp := event.Response
				stopReason = mapStopReason(resp.Status)
				if resp.Usage.TotalTokens > 0 {
					usage = mapUsage(model, resp.Usage)
				}

			case "response.failed":
				return nil, errors.New("openai-responses: response failed")

			case "error":
				return nil, errors.New("openai-responses: " + event.Message)
			}
		}

		err := stream.Err()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}

		if inText {
			push(ai.Event{
				Type:         ai.EventTextEnd,
				ContentIndex: contentIndex,
				Content:      textAccum.String(),
			})
			contentIndex++
		}

		if inThink {
			push(ai.Event{
				Type:         ai.EventThinkEnd,
				ContentIndex: contentIndex,
				Content:      thinkAccum.String(),
			})
			contentIndex++
		}

		if len(toolCalls) > 0 {
			stopReason = ai.StopReasonToolUse
		}

		msg := buildFinalMessage(
			model,
			textAccum.String(),
			thinkAccum.String(),
			toolCalls,
			serverToolCalls,
			usage,
			stopReason,
		)

		log.Debug(
			"[OPENAI-RESPONSES] completed",
			"model", model.ID,
			"stop", stopReason,
			"input", usage.Input,
			"output", usage.Output,
		)

		return msg, nil
	})
}

// GenerateObject generates a structured object in strict JSON Schema mode.
func (p *Provider) GenerateObject(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	schema *jsonschema.Schema,
	opts ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	params := buildParams(model, prompt, opts, p.dialect)
	if schema == nil {
		params.Text.Format = responses.ResponseFormatTextConfigUnionParam{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}
	} else {
		schemaMap, err := schemaToMap(schema)
		if err != nil {
			return nil, fmt.Errorf("openai-responses: %w", err)
		}

		format := responses.ResponseFormatTextConfigParamOfJSONSchema(
			"structured_output",
			schemaMap,
		)
		format.OfJSONSchema.Strict = param.NewOpt(true)
		params.Text.Format = format
	}

	reqOpts := mergeHeaders(model.Headers, opts.Headers)
	if p.dialect == DialectOpenRouter && len(prompt.Tools) > 0 {
		tools := convertOpenRouterTools(prompt.Tools)
		if len(tools) > 0 {
			reqOpts = append(reqOpts, option.WithJSONSet("tools", tools))
		}
	}

	response, err := p.client.Responses.New(ctx, *params, reqOpts...)
	if err != nil {
		return nil, fmt.Errorf("openai-responses: %w", err)
	}

	raw := response.OutputText()
	if raw == "" {
		return nil, errors.New("openai-responses: no object generated")
	}

	usage := mapUsage(model, response.Usage)

	return &ai.ObjectResponse{
		Raw:   raw,
		Usage: usage,
		Model: string(response.Model),
	}, nil
}

// CountTokens counts the input tokens in prompt before the model generates
// a response. OpenAI provides this endpoint only on the native Responses
// API.
func (p *Provider) CountTokens(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) (*ai.TokenCount, error) {
	if p.dialect != DialectOpenAI {
		return nil, errors.New(
			"openai-responses: token counting is only supported by the OpenAI dialect",
		)
	}

	params := responses.InputTokenCountParams{
		Model: param.NewOpt(model.ID),
		Input: responses.InputTokenCountParamsInputUnion{
			OfResponseInputItemArray: convertInput(prompt.Messages),
		},
	}
	if prompt.System != "" {
		params.Instructions = param.NewOpt(prompt.System)
	}
	if len(prompt.Tools) > 0 {
		params.Tools = convertTools(prompt.Tools)
	}
	if opts.ToolChoice != "" {
		params.ToolChoice = convertInputTokenToolChoice(opts.ToolChoice)
	}

	reqOpts := mergeHeaders(model.Headers, opts.Headers)
	response, err := p.client.Responses.InputTokens.Count(ctx, params, reqOpts...)
	if err != nil {
		return nil, fmt.Errorf("openai-responses: %w", err)
	}

	return &ai.TokenCount{Total: int(response.InputTokens)}, nil
}

// schemaToMap converts a jsonschema-go value to the generic JSON schema
// representation of the Responses API.
func schemaToMap(schema *jsonschema.Schema) (map[string]any, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

type streamToolCall struct {
	id        string
	name      string
	itemID    string
	arguments strings.Builder
}

func buildParams(
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
	dialect Dialect,
) *responses.ResponseNewParams {
	params := &responses.ResponseNewParams{
		Model: shared.ResponsesModel(model.ID),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: convertInput(prompt.Messages),
		},
		Store: param.NewOpt(false),
	}

	// Codex requires a non-empty instructions field. If the caller supplies
	// no system prompt, use a default value.
	switch {
	case prompt.System != "":
		params.Instructions = param.NewOpt(prompt.System)
	case dialect == DialectCodex:
		params.Instructions = param.NewOpt(codexDefaultInstructions)
	}

	if opts.MaxTokens != nil {
		params.MaxOutputTokens = param.NewOpt(int64(*opts.MaxTokens))
	}

	if opts.Temperature != nil {
		params.Temperature = param.NewOpt(*opts.Temperature)
	}

	if level := ai.EffectiveThinkingLevel(model, opts.ThinkingLevel); level != ai.ThinkingOff {
		params.Reasoning = shared.ReasoningParam{
			Effort:  mapThinkingLevel(level),
			Summary: shared.ReasoningSummaryAuto,
		}
		params.Include = []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		}
	}

	if len(prompt.Tools) > 0 {
		// In the OpenRouter dialect, the StreamText callsite adds the
		// tools array with option.WithJSONSet. Leave params.Tools empty
		// here.
		//
		// In the Codex dialect, the backend does not support server tools
		// such as web_search. Remove them before the request.
		switch dialect {
		case DialectOpenRouter:
			// The callsite adds the tools with WithJSONSet.
		case DialectCodex:
			params.Tools = convertTools(filterFunctionTools(prompt.Tools))
		default:
			params.Tools = convertTools(prompt.Tools)
		}
		if opts.ToolChoice != "" {
			params.ToolChoice = convertToolChoice(opts.ToolChoice)
		}
	}

	// Forward the session ID as the prompt_cache_key for cache affinity.
	// The OpenAI Responses API caches automatically. The key only improves
	// prefix matching across requests. If the caller turns caching off, the
	// request omits the key.
	cacheRetention := ai.ResolveCacheRetention(opts.CacheRetention)
	if cacheRetention != ai.CacheRetentionNone && opts.SessionID != "" {
		params.PromptCacheKey = param.NewOpt(opts.SessionID)
	}
	if cacheRetention == ai.CacheRetentionLong {
		params.PromptCacheRetention = responses.ResponseNewParamsPromptCacheRetention24h
	}

	return params
}

func mergeHeaders(
	modelHeaders map[string]string,
	optsHeaders map[string]string,
) []option.RequestOption {
	merged := make(map[string]string)
	maps.Copy(merged, modelHeaders)
	maps.Copy(merged, optsHeaders)

	opts := make([]option.RequestOption, 0, len(merged))
	for k, v := range merged {
		opts = append(opts, option.WithHeader(k, v))
	}
	return opts
}

func buildFinalMessage(
	model ai.Model,
	text string,
	thinking string,
	toolCalls map[int64]*streamToolCall,
	serverToolCalls []ai.ToolCall,
	usage ai.Usage,
	stopReason ai.StopReason,
) *ai.Message {
	var content []ai.Content

	if thinking != "" {
		content = append(content, ai.Thinking{Thinking: thinking})
	}
	if text != "" {
		content = append(content, ai.Text{Text: text})
	}
	for _, tc := range toolCalls {
		var args map[string]any
		if tc.arguments.Len() > 0 {
			_ = json.Unmarshal([]byte(tc.arguments.String()), &args)
		}
		content = append(content, ai.ToolCall{
			ID:        tc.id,
			Name:      tc.name,
			Arguments: args,
		})
	}
	for _, sc := range serverToolCalls {
		content = append(content, sc)
	}

	return &ai.Message{
		Role:       ai.RoleAssistant,
		Content:    content,
		API:        ID,
		Provider:   ID,
		Model:      model.ID,
		Usage:      usage,
		StopReason: stopReason,
		Timestamp:  time.Now(),
	}
}

// serverToolNameByType extracts the caller-supplied [ai.ToolInfo.Name] for
// each server tool in the prompt. The key of the map is the
// [ai.ServerToolType]. The SSE pipeline uses the result to rewrite raw
// provider item types ("web_search_call", "openrouter:web_search") to the
// canonical name that the caller registered ("WebSearch"). Server tools
// then persist with the same shape as function tools.
func serverToolNameByType(tools []ai.ToolInfo) map[ai.ServerToolType]string {
	if len(tools) == 0 {
		return nil
	}
	out := make(map[ai.ServerToolType]string, len(tools))
	for _, t := range tools {
		if t.Kind != ai.ToolKindServer {
			continue
		}
		if t.Name != "" && t.ServerType != "" {
			out[t.ServerType] = t.Name
		}
	}
	return out
}

// canonicalServerToolName returns the caller-registered name for a server
// tool of type st. If the caller registered no name, it returns fallback.
// This is rare. It happens only when the model invents a tool that the
// caller never declared.
func canonicalServerToolName(
	st ai.ServerToolType,
	names map[ai.ServerToolType]string,
	fallback string,
) string {
	if name, ok := names[st]; ok && name != "" {
		return name
	}
	return fallback
}

// serverTypeForOpenRouterItem maps an OpenRouter `openrouter:*` item type
// in a Responses stream to the canonical pi-go [ai.ServerToolType]. For an
// unknown type, it returns the empty string.
func serverTypeForOpenRouterItem(itemType string) ai.ServerToolType {
	suffix := strings.TrimPrefix(itemType, "openrouter:")
	switch suffix {
	case "web_search":
		return ai.ServerToolWebSearch
	case "web_fetch":
		return ai.ServerToolWebFetch
	case "datetime":
		return ai.ServerToolDateTime
	default:
		return ""
	}
}

// buildOpenRouterServerToolCall converts a completed `openrouter:*` output
// item into an [ai.ToolCall] with Server=true. It reads the common argument
// fields (`query`, `url`, `timezone`) from the raw payload when they exist.
// The call then has the same `Arguments` shape as a function tool.
// Output.Raw always holds the verbatim JSON for callers that need more
// fields.
func buildOpenRouterServerToolCall(item responses.ResponseOutputItemUnion) ai.ToolCall {
	raw := item.RawJSON()
	args := extractOpenRouterArgs(raw)
	return ai.ToolCall{
		ID:         item.ID,
		Name:       item.Type,
		Arguments:  args,
		Server:     true,
		ServerType: serverTypeForOpenRouterItem(item.Type),
		Output: &ai.ServerToolOutput{
			Content: openRouterOutputSummary(item.Type, raw),
			Raw:     json.RawMessage(raw),
			IsError: item.Status == "failed",
		},
	}
}

// extractOpenRouterArgs reads the common server-tool arguments from a raw
// OpenRouter output item payload. If the payload holds no known field, it
// returns nil. The caller can then tell "no args" from "empty args".
func extractOpenRouterArgs(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	args := map[string]any{}
	for _, key := range []string{"query", "url", "timezone"} {
		if v := gjson.Get(raw, key); v.Exists() && v.String() != "" {
			args[key] = v.String()
		}
		// Some payloads nest under "action" or "input".
		if v := gjson.Get(raw, "action."+key); v.Exists() && v.String() != "" {
			args[key] = v.String()
		}
		if v := gjson.Get(raw, "input."+key); v.Exists() && v.String() != "" {
			args[key] = v.String()
		}
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

// openRouterOutputSummary returns a one-line description of what a server
// tool did in this turn. If the shape of the payload is not known, it
// returns the empty string. Output.Raw still holds the full JSON.
func openRouterOutputSummary(itemType, raw string) string {
	if raw == "" {
		return ""
	}
	switch itemType {
	case "openrouter:web_search":
		if q := gjson.Get(raw, "query"); q.Exists() {
			return "search: " + q.String()
		}
		if q := gjson.Get(raw, "input.query"); q.Exists() {
			return "search: " + q.String()
		}
	case "openrouter:web_fetch":
		if u := gjson.Get(raw, "url"); u.Exists() {
			return "fetch: " + u.String()
		}
		if u := gjson.Get(raw, "input.url"); u.Exists() {
			return "fetch: " + u.String()
		}
	case "openrouter:datetime":
		if tz := gjson.Get(raw, "timezone"); tz.Exists() {
			return "datetime: " + tz.String()
		}
	}
	return ""
}

// serverTypeForItem maps a Responses API item type to the canonical pi-go
// [ai.ServerToolType]. For an unknown item type, it returns the empty
// string.
func serverTypeForItem(itemType string) ai.ServerToolType {
	switch itemType {
	case "web_search_call":
		return ai.ServerToolWebSearch
	case "code_interpreter_call":
		return ai.ServerToolCodeExecution
	case "file_search_call":
		return ai.ServerToolFileSearch
	case "computer_call":
		return ai.ServerToolComputer
	case "mcp_call":
		return ai.ServerToolMCP
	default:
		return ""
	}
}

// buildServerToolCall converts a completed [responses.ResponseOutputItemUnion]
// of a server-tool variant into an [ai.ToolCall] with Server=true. It fills
// Output from the status of the item and from the per-tool fields.
func buildServerToolCall(item responses.ResponseOutputItemUnion) ai.ToolCall {
	tc := ai.ToolCall{
		ID:         item.ID,
		Name:       item.Type,
		Server:     true,
		ServerType: serverTypeForItem(item.Type),
	}

	switch item.Type {
	case "web_search_call":
		// If the item holds a search query, put it in Arguments.
		if q := item.Action.Query; q != "" {
			tc.Arguments = map[string]any{"query": q}
		}
		tc.Output = &ai.ServerToolOutput{
			Content: webSearchActionDescription(item),
			Raw:     json.RawMessage(item.RawJSON()),
			IsError: item.Status == "failed",
		}

	case "code_interpreter_call":
		if item.Code != "" {
			tc.Arguments = map[string]any{"code": item.Code}
		}
		tc.Output = &ai.ServerToolOutput{
			Content: codeInterpreterOutputs(item),
			Raw:     json.RawMessage(item.RawJSON()),
			IsError: item.Status == "failed",
		}

	default:
		tc.Output = &ai.ServerToolOutput{
			Content: string(item.Status),
			Raw:     json.RawMessage(item.RawJSON()),
			IsError: item.Status == "failed",
		}
	}

	return tc
}

// webSearchActionDescription returns a one-line summary of what the web
// search tool did in this turn (search, open_page, or find). A caller can
// show this summary to the user.
func webSearchActionDescription(item responses.ResponseOutputItemUnion) string {
	if item.Action.Query != "" {
		return "search: " + item.Action.Query
	}
	if item.Action.URL != "" {
		return "open: " + item.Action.URL
	}
	return string(item.Status)
}

// codeInterpreterOutputs joins the stdout, stderr, and log outputs of the
// code interpreter into one string. The original code lives in Arguments.
func codeInterpreterOutputs(item responses.ResponseOutputItemUnion) string {
	var b strings.Builder
	for i, out := range item.Outputs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(out.RawJSON())
	}
	return b.String()
}
