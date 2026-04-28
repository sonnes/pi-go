// Package openairesponses provides an OpenAI Responses API provider.
package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// Dialect selects between request/response shapes that share the OpenAI
// Responses API surface but diverge in tool naming and SSE event taxonomy.
type Dialect int

const (
	// DialectOpenAI is OpenAI's native Responses API. Default.
	DialectOpenAI Dialect = iota
	// DialectOpenRouter targets OpenRouter's `/v1/responses` endpoint:
	// server tools use the `openrouter:*` namespace, and text deltas arrive
	// on `response.content_part.delta` instead of `response.output_text.delta`.
	// See [docs/plans/2026-04-28-openrouter-responses-dialect.md] for the
	// observed differences this dialect compensates for.
	DialectOpenRouter
)

// Provider implements [ai.Provider] for OpenAI's Responses API.
//
// Two providers may share the same [Provider.API] identifier
// ("openai-responses") when one targets OpenAI and another targets
// OpenRouter via [DialectOpenRouter]. Callers must bind a specific provider
// per agent (e.g. `agent.WithProvider(p)`) rather than relying on
// [ai.GetProvider] global registry lookup.
type Provider struct {
	client  *openai.Client
	dialect Dialect
}

// Verify interface compliance.
var _ ai.Provider = (*Provider)(nil)

// New creates a new OpenAI Responses provider targeting OpenAI's native API.
// For OpenRouter use [NewForOpenRouter].
func New(opts ...option.RequestOption) *Provider {
	opts = append(
		[]option.RequestOption{option.WithMaxRetries(0)},
		opts...,
	)
	client := openai.NewClient(opts...)
	return &Provider{client: &client, dialect: DialectOpenAI}
}

// NewForOpenRouter creates a Responses-API provider that talks to OpenRouter.
// Callers should pass [option.WithBaseURL]("https://openrouter.ai/api/v1") and
// [option.WithAPIKey] for their OpenRouter key.
//
// Server tools are translated to the `openrouter:*` namespace; SSE events
// from `/v1/responses` are mapped onto pi-go's standard event stream. See
// [Dialect] for the full list of compensated differences.
func NewForOpenRouter(opts ...option.RequestOption) *Provider {
	p := New(opts...)
	p.dialect = DialectOpenRouter
	return p
}

// API returns the provider API identifier. Both dialects share the same ID;
// callers bind a specific provider per agent rather than via global registry
// lookup.
func (p *Provider) API() string {
	return "openai-responses"
}

// StreamText streams a text response using the Responses API.
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

	// For the OpenRouter dialect, override the request body's "tools" field
	// with the openrouter:* server-tool shapes since the OpenAI SDK's typed
	// ToolUnionParam can't express them. See convertOpenRouterTools.
	if p.dialect == DialectOpenRouter && len(prompt.Tools) > 0 {
		orTools := convertOpenRouterTools(prompt.Tools)
		if len(orTools) > 0 {
			reqOpts = append(reqOpts, option.WithJSONSet("tools", orTools))
		}
	}

	return ai.NewEventStream(func(push func(ai.Event)) {
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
			case "response.reasoning_summary_text.delta":
				delta := event.Delta.OfString
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

			case "response.reasoning_summary_part.done":
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
				delta := event.Delta.OfString
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
			// rather than output_text events. The SDK's typed union doesn't
			// know about content_part.delta, but the flat fields populate
			// from the JSON payload regardless.
			case "response.content_part.delta":
				delta := event.Delta.OfString
				if delta == "" {
					// Some OpenRouter payloads put the text under part.text
					// for the initial chunk; fall back to that.
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
				// Idempotent close: OpenAI emits this after output_text.done
				// (so inText is already false); OpenRouter emits it as the
				// actual text-block terminator.
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
				case "web_search_call", "code_interpreter_call":
					if inText {
						inText = false
						push(ai.Event{
							Type:         ai.EventTextEnd,
							ContentIndex: contentIndex,
							Content:      textAccum.String(),
						})
						contentIndex++
					}
					push(ai.Event{
						Type:         ai.EventToolStart,
						ContentIndex: contentIndex,
						ToolCall: &ai.ToolCall{
							ID:         event.Item.ID,
							Name:       event.Item.Type,
							Server:     true,
							ServerType: serverTypeForItem(event.Item.Type),
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
						push(ai.Event{
							Type:         ai.EventToolStart,
							ContentIndex: contentIndex,
							ToolCall: &ai.ToolCall{
								ID:         event.Item.ID,
								Name:       event.Item.Type,
								Server:     true,
								ServerType: serverTypeForOpenRouterItem(event.Item.Type),
							},
						})
					}
				}

			case "response.output_item.done":
				switch event.Item.Type {
				case "web_search_call", "code_interpreter_call":
					call := buildServerToolCall(event.Item)
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
				delta := event.Delta.OfString
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
					usage = mapUsage(resp.Usage)
				}

			case "response.failed":
				push(ai.Event{
					Type: ai.EventError,
					Err:  errors.New("openai-responses: response failed"),
				})
				return

			case "error":
				push(ai.Event{
					Type: ai.EventError,
					Err:  errors.New("openai-responses: " + event.Message),
				})
				return
			}
		}

		err := stream.Err()
		if err != nil && !errors.Is(err, io.EOF) {
			push(ai.Event{
				Type: ai.EventError,
				Err:  err,
			})
			return
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

		push(ai.Event{
			Type:       ai.EventDone,
			Message:    msg,
			StopReason: stopReason,
		})
	})
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

	if prompt.System != "" {
		params.Instructions = param.NewOpt(prompt.System)
	}

	if opts.MaxTokens != nil {
		params.MaxOutputTokens = param.NewOpt(int64(*opts.MaxTokens))
	}

	if opts.Temperature != nil {
		params.Temperature = param.NewOpt(*opts.Temperature)
	}

	if opts.ThinkingLevel != "" {
		params.Reasoning = shared.ReasoningParam{
			Effort:  mapThinkingLevel(opts.ThinkingLevel),
			Summary: shared.ReasoningSummaryAuto,
		}
		params.Include = []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		}
	}

	if len(prompt.Tools) > 0 {
		// In the OpenRouter dialect the tools array is injected via
		// option.WithJSONSet at the StreamText callsite, so leave
		// params.Tools empty here.
		if dialect != DialectOpenRouter {
			params.Tools = convertTools(prompt.Tools)
		}
		if opts.ToolChoice != "" {
			params.ToolChoice = convertToolChoice(opts.ToolChoice)
		}
	}

	// Forward the session ID as the prompt_cache_key for cache affinity.
	// OpenAI Responses caching is automatic; the key only strengthens prefix
	// matching across requests. Suppressed when caching is explicitly disabled.
	cacheRetention := ai.ResolveCacheRetention(opts.CacheRetention)
	if cacheRetention != ai.CacheRetentionNone && opts.SessionID != "" {
		params.PromptCacheKey = param.NewOpt(opts.SessionID)
	}

	return params
}

func mergeHeaders(
	modelHeaders map[string]string,
	optsHeaders map[string]string,
) []option.RequestOption {
	merged := make(map[string]string)
	for k, v := range modelHeaders {
		merged[k] = v
	}
	for k, v := range optsHeaders {
		merged[k] = v
	}

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
		API:        "openai-responses",
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      usage,
		StopReason: stopReason,
		Timestamp:  time.Now(),
	}
}

// serverTypeForOpenRouterItem maps an OpenRouter `openrouter:*` item type
// in a Responses stream to the canonical pi-go [ai.ServerToolType]. Unknown
// types yield the empty string.
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
// item into an [ai.ToolCall] with Server=true. Output.Raw is the verbatim
// JSON for callers that need structured fields; Output.Content is left
// empty until we have richer cassette-derived knowledge of the payload
// shapes per OpenRouter tool.
func buildOpenRouterServerToolCall(item responses.ResponseOutputItemUnion) ai.ToolCall {
	raw := item.RawJSON()
	return ai.ToolCall{
		ID:         item.ID,
		Name:       item.Type,
		Server:     true,
		ServerType: serverTypeForOpenRouterItem(item.Type),
		Output: &ai.ServerToolOutput{
			Content: "",
			Raw:     json.RawMessage(raw),
			IsError: item.Status == "failed",
		},
	}
}

// serverTypeForItem maps a Responses API item type to the canonical pi-go
// [ai.ServerToolType]. Unknown item types yield the empty string.
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
// of a server-tool variant into an [ai.ToolCall] with Server=true and Output
// populated from the item's status and per-tool fields.
func buildServerToolCall(item responses.ResponseOutputItemUnion) ai.ToolCall {
	tc := ai.ToolCall{
		ID:         item.ID,
		Name:       item.Type,
		Server:     true,
		ServerType: serverTypeForItem(item.Type),
	}

	switch item.Type {
	case "web_search_call":
		// Capture the search query in Arguments when available.
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
	}

	return tc
}

// webSearchActionDescription renders a one-line summary of what the web search
// tool did in this turn (search/open_page/find), suitable for display.
func webSearchActionDescription(item responses.ResponseOutputItemUnion) string {
	if item.Action.Query != "" {
		return "search: " + item.Action.Query
	}
	if item.Action.URL != "" {
		return "open: " + item.Action.URL
	}
	return string(item.Status)
}

// codeInterpreterOutputs renders the code interpreter's stdout/stderr/log
// outputs as a concatenated string. The original code lives in Arguments.
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
