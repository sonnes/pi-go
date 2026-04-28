// Package anthropic provides an Anthropic Messages API provider for the AI SDK.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/jsonschema-go/jsonschema"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/oauth"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// Compile-time interface checks.
var (
	_ ai.Provider       = (*Provider)(nil)
	_ ai.ObjectProvider = (*Provider)(nil)
)

// Provider implements ai.Provider for the Anthropic Messages API.
type Provider struct {
	client  anthropic.Client
	baseURL string
}

// New creates a new Anthropic provider.
func New(opts ...Option) *Provider {
	o := &options{
		headers: make(map[string]string),
	}
	for _, opt := range opts {
		opt(o)
	}

	clientOpts := []option.RequestOption{
		option.WithMaxRetries(0),
	}

	if o.oauthCreds != nil {
		clientOpts = append(
			clientOpts,
			option.WithAuthToken(o.oauthCreds.AccessToken),
		)
		transport := NewOAuthTransport(o.oauthClientID, *o.oauthCreds, o.oauthOpts...)
		if o.httpClient != nil {
			transport.Base = o.httpClient.Transport
		}
		o.httpClient = &http.Client{Transport: transport}
	} else if o.apiKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(o.apiKey))
	}
	if o.baseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(o.baseURL))
	}
	for k, v := range o.headers {
		clientOpts = append(clientOpts, option.WithHeader(k, v))
	}
	if o.httpClient != nil {
		clientOpts = append(clientOpts, option.WithHTTPClient(o.httpClient))
	}

	return &Provider{
		client:  anthropic.NewClient(clientOpts...),
		baseURL: o.baseURL,
	}
}

// API returns the provider API identifier.
func (p *Provider) API() string {
	return "anthropic-messages"
}

// StreamText streams a text response from the Anthropic Messages API.
func (p *Provider) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) {
		params, reqOpts := buildParams(model, prompt, opts, p.baseURL)

		log.Debug(
			"[ANTHROPIC] starting stream",
			"model", model.ID,
			"messages", len(prompt.Messages),
			"tools", len(prompt.Tools),
		)

		stream := p.client.Messages.NewStreaming(ctx, params, reqOpts...)
		acc := anthropic.Message{}

		// Track block types by index for content_block_stop.
		var blockTypes []string

		// Pending server-tool calls awaiting their result block. Server tools
		// stream as two paired blocks (server_tool_use + web_search_tool_result);
		// we merge them into a single ai.ToolCall with Output populated when the
		// result arrives, and emit one EventToolEnd at that point.
		pendingServerCalls := map[string]*ai.ToolCall{}

		for stream.Next() {
			chunk := stream.Current()
			_ = acc.Accumulate(chunk)

			switch chunk.Type {
			case "content_block_start":
				blockTypes = append(blockTypes, chunk.ContentBlock.Type)
				switch chunk.ContentBlock.Type {
				case "text":
					push(ai.Event{
						Type:         ai.EventTextStart,
						ContentIndex: int(chunk.Index),
					})
				case "thinking":
					push(ai.Event{
						Type:         ai.EventThinkStart,
						ContentIndex: int(chunk.Index),
					})
				case "tool_use", "server_tool_use":
					push(ai.Event{
						Type:         ai.EventToolStart,
						ContentIndex: int(chunk.Index),
					})
				}

			case "content_block_delta":
				switch chunk.Delta.Type {
				case "text_delta":
					push(ai.Event{
						Type:         ai.EventTextDelta,
						ContentIndex: int(chunk.Index),
						Delta:        chunk.Delta.Text,
					})
				case "thinking_delta":
					push(ai.Event{
						Type:         ai.EventThinkDelta,
						ContentIndex: int(chunk.Index),
						Delta:        chunk.Delta.Thinking,
					})
				case "signature_delta":
					// Signature is accumulated by acc.Accumulate; no separate event.
				case "input_json_delta":
					push(ai.Event{
						Type:         ai.EventToolDelta,
						ContentIndex: int(chunk.Index),
						Delta:        chunk.Delta.PartialJSON,
					})
				}

			case "content_block_stop":
				idx := int(chunk.Index)
				if idx < len(blockTypes) {
					switch blockTypes[idx] {
					case "text":
						var text string
						if idx < len(acc.Content) {
							text = acc.Content[idx].Text
						}
						push(ai.Event{
							Type:         ai.EventTextEnd,
							ContentIndex: idx,
							Content:      text,
						})
					case "thinking":
						push(ai.Event{
							Type:         ai.EventThinkEnd,
							ContentIndex: idx,
						})
					case "tool_use":
						if idx < len(acc.Content) {
							cb := acc.Content[idx]
							var args map[string]any
							if len(cb.Input) > 0 {
								_ = json.Unmarshal(cb.Input, &args)
							}
							push(ai.Event{
								Type:         ai.EventToolEnd,
								ContentIndex: idx,
								ToolCall: &ai.ToolCall{
									ID:        cb.ID,
									Name:      cb.Name,
									Arguments: args,
								},
							})
						}
					case "server_tool_use":
						// Stash the call; emit when its result block arrives.
						if idx < len(acc.Content) {
							stu := acc.Content[idx].AsServerToolUse()
							pendingServerCalls[stu.ID] = &ai.ToolCall{
								ID:         stu.ID,
								Name:       string(stu.Name),
								Arguments:  serverToolInputToMap(stu.Input),
								Server:     true,
								ServerType: serverTypeForName(string(stu.Name)),
							}
						}
					case "web_search_tool_result":
						if idx < len(acc.Content) {
							res := acc.Content[idx].AsWebSearchToolResult()
							call := pendingServerCalls[res.ToolUseID]
							if call == nil {
								call = &ai.ToolCall{
									ID:         res.ToolUseID,
									Server:     true,
									ServerType: ai.ServerToolWebSearch,
								}
							}
							call.Output = buildWebSearchOutput(res)
							delete(pendingServerCalls, res.ToolUseID)
							push(ai.Event{
								Type:         ai.EventToolEnd,
								ContentIndex: idx,
								ToolCall:     call,
							})
						}
					}
				}
			}
		}

		// Drain any server-tool calls that never received a result (rare; usually
		// indicates an upstream error mid-stream). Emit them with no Output.
		for _, call := range pendingServerCalls {
			push(ai.Event{
				Type:     ai.EventToolEnd,
				ToolCall: call,
			})
		}

		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			push(ai.Event{
				Type: ai.EventError,
				Err:  fmt.Errorf("anthropic: %w", err),
			})
			return
		}

		msg := buildMessage(model, &acc)

		push(ai.Event{
			Type:       ai.EventDone,
			Message:    msg,
			StopReason: msg.StopReason,
		})
	})
}

// buildParams constructs Anthropic MessageNewParams from types.
func buildParams(
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
	baseURL string,
) (anthropic.MessageNewParams, []option.RequestOption) {
	maxTokens := int64(4096)
	if opts.MaxTokens != nil {
		maxTokens = int64(*opts.MaxTokens)
	}
	if model.MaxTokens > 0 && opts.MaxTokens == nil {
		maxTokens = int64(model.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model.ID),
		MaxTokens: maxTokens,
	}

	cc, cacheEnabled := cacheMarker(opts, baseURL)

	if prompt.System != "" {
		sysBlock := anthropic.TextBlockParam{Text: prompt.System}
		if cacheEnabled {
			sysBlock.CacheControl = cc
		}
		params.System = []anthropic.TextBlockParam{sysBlock}
	}

	params.Messages = convertMessages(prompt.Messages)
	if cacheEnabled {
		applyCacheControlToLastBlock(params.Messages, cc)
	}

	if opts.Temperature != nil && opts.ThinkingLevel == "" {
		params.Temperature = param.NewOpt(*opts.Temperature)
	}

	if opts.ThinkingLevel != "" {
		budget := thinkingBudget(opts.ThinkingLevel)
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
		params.MaxTokens = maxTokens + budget
		// Temperature not supported with thinking.
		params.Temperature = param.Opt[float64]{}
	}

	if len(prompt.Tools) > 0 {
		params.Tools = convertTools(prompt.Tools)
	}

	if opts.ToolChoice != "" {
		params.ToolChoice = convertToolChoice(opts.ToolChoice)
	}

	var reqOpts []option.RequestOption
	for k, v := range model.Headers {
		reqOpts = append(reqOpts, option.WithHeader(k, v))
	}
	for k, v := range opts.Headers {
		reqOpts = append(reqOpts, option.WithHeader(k, v))
	}

	return params, reqOpts
}

// thinkingBudget maps a ThinkingLevel to a token budget.
func thinkingBudget(level ai.ThinkingLevel) int64 {
	switch level {
	case ai.ThinkingMinimal, ai.ThinkingLow:
		return 1024
	case ai.ThinkingMedium:
		return 4096
	case ai.ThinkingHigh:
		return 8192
	case ai.ThinkingXHigh:
		return 16384
	default:
		return 4096
	}
}

// buildMessage constructs the final ai.Message from the accumulated response.
//
// Server-tool blocks (server_tool_use + web_search_tool_result) are merged into
// a single ai.ToolCall with Server=true and Output populated. Function tool_use
// blocks remain as bare ai.ToolCall (Output nil; client executes them).
func buildMessage(model ai.Model, acc *anthropic.Message) *ai.Message {
	var content []ai.Content

	// Maps server_tool_use ID to the index in content where its merged ToolCall
	// lives, so the matching result block can attach Output without re-scanning.
	serverIdx := map[string]int{}

	for _, block := range acc.Content {
		switch block.Type {
		case "text":
			content = append(content, ai.Text{Text: block.Text})
		case "thinking":
			content = append(content, ai.Thinking{
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "tool_use":
			var args map[string]any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &args)
			}
			content = append(content, ai.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		case "server_tool_use":
			stu := block.AsServerToolUse()
			content = append(content, ai.ToolCall{
				ID:         stu.ID,
				Name:       string(stu.Name),
				Arguments:  serverToolInputToMap(stu.Input),
				Server:     true,
				ServerType: serverTypeForName(string(stu.Name)),
			})
			serverIdx[stu.ID] = len(content) - 1
		case "web_search_tool_result":
			res := block.AsWebSearchToolResult()
			out := buildWebSearchOutput(res)
			if i, ok := serverIdx[res.ToolUseID]; ok {
				tc := content[i].(ai.ToolCall)
				tc.Output = out
				content[i] = tc
				delete(serverIdx, res.ToolUseID)
			} else {
				content = append(content, ai.ToolCall{
					ID:         res.ToolUseID,
					Server:     true,
					ServerType: ai.ServerToolWebSearch,
					Output:     out,
				})
			}
		}
	}

	total := int(acc.Usage.InputTokens + acc.Usage.OutputTokens)
	usage := ai.Usage{
		Input:      int(acc.Usage.InputTokens),
		Output:     int(acc.Usage.OutputTokens),
		CacheRead:  int(acc.Usage.CacheReadInputTokens),
		CacheWrite: int(acc.Usage.CacheCreationInputTokens),
		Total:      total,
	}
	usage.Cost = ai.CalculateCost(model, usage)

	return &ai.Message{
		Role:       ai.RoleAssistant,
		Content:    content,
		API:        model.API,
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      usage,
		StopReason: mapStopReason(string(acc.StopReason)),
		Timestamp:  time.Now(),
	}
}

// mapStopReason converts an Anthropic stop reason to the StopReason.
func mapStopReason(reason string) ai.StopReason {
	switch reason {
	case "end_turn", "pause_turn", "stop_sequence":
		return ai.StopReasonStop
	case "max_tokens":
		return ai.StopReasonLength
	case "tool_use":
		return ai.StopReasonToolUse
	default:
		return ai.StopReasonStop
	}
}

// serverTypeForName maps an Anthropic server-tool block name to the canonical
// pi-go [ai.ServerToolType]. Unknown names yield the empty type.
func serverTypeForName(name string) ai.ServerToolType {
	switch name {
	case "web_search":
		return ai.ServerToolWebSearch
	default:
		return ai.ServerToolType(name)
	}
}

// serverToolInputToMap converts a server_tool_use Input (typed as `any` by
// the SDK because the shape varies per tool) to a map[string]any so it can
// fit pi-go's [ai.ToolCall.Arguments].
func serverToolInputToMap(input any) map[string]any {
	if input == nil {
		return nil
	}
	if m, ok := input.(map[string]any); ok {
		return m
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

// buildWebSearchOutput converts a [WebSearchToolResultBlock] to an
// [ai.ServerToolOutput]. Successful results are rendered as a numbered list
// of "Title — URL" entries; errors are surfaced via IsError with the error
// code in Content. Raw retains the provider's full JSON for callers that want
// to extract per-result fields like encrypted_content for follow-up turns.
func buildWebSearchOutput(res anthropic.WebSearchToolResultBlock) *ai.ServerToolOutput {
	out := &ai.ServerToolOutput{
		Raw: json.RawMessage(res.RawJSON()),
	}

	if errBlock := res.Content.AsResponseWebSearchToolResultError(); errBlock.ErrorCode != "" {
		out.IsError = true
		out.Content = string(errBlock.ErrorCode)
		return out
	}

	results := res.Content.OfWebSearchResultBlockArray
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%d. %s — %s", i+1, r.Title, r.URL)
	}
	out.Content = b.String()
	return out
}

// Option configures the Anthropic provider.
type Option func(*options)

type options struct {
	apiKey        string
	oauthClientID string
	oauthCreds    *oauth.Credentials
	oauthOpts     []oauth.TransportOption
	baseURL       string
	headers       map[string]string
	httpClient    *http.Client
}

// WithAPIKey sets the API key for authentication.
func WithAPIKey(apiKey string) Option {
	return func(o *options) { o.apiKey = apiKey }
}

// WithOAuth configures the provider for OAuth Bearer token authentication.
// It sets up the auth token, OAuth-specific headers, and automatic token
// refresh via the [oauth.Transport] middleware. Additional transport options
// (e.g. [oauth.WithOnRefresh] for credential persistence) can be passed.
func WithOAuth(clientID string, creds oauth.Credentials, opts ...oauth.TransportOption) Option {
	return func(o *options) {
		o.oauthClientID = clientID
		o.oauthCreds = &creds
		o.oauthOpts = opts
	}
}

// WithBaseURL sets a custom base URL for the API.
func WithBaseURL(baseURL string) Option {
	return func(o *options) { o.baseURL = baseURL }
}

// WithHeaders sets additional HTTP headers for requests.
func WithHeaders(headers map[string]string) Option {
	return func(o *options) {
		for k, v := range headers {
			o.headers[k] = v
		}
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) { o.httpClient = client }
}

// GenerateObject generates a structured object using the tool-use trick.
// It creates a synthetic tool from the schema and forces the model to call it,
// then extracts the structured JSON from the tool call input.
func (p *Provider) GenerateObject(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	schema *jsonschema.Schema,
	opts ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	log.Debug(
		"[ANTHROPIC] generating object",
		"model", model.ID,
		"messages", len(prompt.Messages),
	)

	properties, required, err := extractSchemaFields(schema)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}

	structuredOutputTool := anthropic.ToolParam{
		Name:        "structured_output",
		Description: anthropic.String("Output the structured data"),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: properties,
			Required:   required,
		},
	}

	maxTokens := int64(4096)
	if opts.MaxTokens != nil {
		maxTokens = int64(*opts.MaxTokens)
	}
	if model.MaxTokens > 0 && opts.MaxTokens == nil {
		maxTokens = int64(model.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model.ID),
		MaxTokens: maxTokens,
		Tools:     []anthropic.ToolUnionParam{{OfTool: &structuredOutputTool}},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Type: "tool",
				Name: "structured_output",
			},
		},
	}

	cc, cacheEnabled := cacheMarker(opts, p.baseURL)

	if prompt.System != "" {
		sysBlock := anthropic.TextBlockParam{Text: prompt.System}
		if cacheEnabled {
			sysBlock.CacheControl = cc
		}
		params.System = []anthropic.TextBlockParam{sysBlock}
	}

	params.Messages = convertMessages(prompt.Messages)
	if cacheEnabled {
		applyCacheControlToLastBlock(params.Messages, cc)
	}

	if opts.Temperature != nil {
		params.Temperature = param.NewOpt(*opts.Temperature)
	}

	var reqOpts []option.RequestOption
	for k, v := range model.Headers {
		reqOpts = append(reqOpts, option.WithHeader(k, v))
	}
	for k, v := range opts.Headers {
		reqOpts = append(reqOpts, option.WithHeader(k, v))
	}

	response, err := p.client.Messages.New(ctx, params, reqOpts...)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}

	var rawJSON string
	for _, block := range response.Content {
		if block.Type == "tool_use" {
			rawJSON = string(block.Input)
			break
		}
	}

	if rawJSON == "" {
		return nil, fmt.Errorf("anthropic: no structured output in response")
	}

	total := int(response.Usage.InputTokens + response.Usage.OutputTokens)
	usage := ai.Usage{
		Input:      int(response.Usage.InputTokens),
		Output:     int(response.Usage.OutputTokens),
		CacheRead:  int(response.Usage.CacheReadInputTokens),
		CacheWrite: int(response.Usage.CacheCreationInputTokens),
		Total:      total,
	}
	usage.Cost = ai.CalculateCost(model, usage)

	log.Debug(
		"[ANTHROPIC] object completed",
		"model", model.ID,
		"input", usage.Input,
		"output", usage.Output,
	)

	return &ai.ObjectResponse{
		Raw:   rawJSON,
		Usage: usage,
		Model: string(response.Model),
	}, nil
}

// extractSchemaFields extracts properties and required fields from a JSON schema.
func extractSchemaFields(schema *jsonschema.Schema) (properties any, required []string, err error) {
	if schema == nil {
		return nil, nil, nil
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	if props, ok := schemaMap["properties"]; ok {
		properties = props
	}
	if req, ok := schemaMap["required"]; ok {
		if reqArr, ok := req.([]any); ok {
			for _, r := range reqArr {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}
	}

	return properties, required, nil
}
