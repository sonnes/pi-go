// Package openai provides an OpenAI provider for the AI SDK.
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// Provider implements ai.Provider for OpenAI's chat completions API.
type Provider struct {
	client *openai.Client
}

// Verify interface compliance.
var (
	_ ai.Provider       = (*Provider)(nil)
	_ ai.ObjectProvider = (*Provider)(nil)
	_ ai.ImageProvider  = (*Provider)(nil)
)

// New creates a new OpenAI provider.
func New(opts ...option.RequestOption) *Provider {
	opts = append([]option.RequestOption{option.WithMaxRetries(0)}, opts...)
	client := openai.NewClient(opts...)
	return &Provider{client: &client}
}

// API returns the provider API identifier.
func (p *Provider) API() string {
	return "openai-completions"
}

// StreamText streams a text response from the given model.
func (p *Provider) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) *ai.EventStream {
	log.Debug(
		"[OPENAI] starting stream",
		"model", model.ID,
		"messages", len(prompt.Messages),
		"tools", len(prompt.Tools),
	)

	params := buildParams(model, prompt, opts)

	compat := getCompat(model)
	if compat.SupportsUsageInStreaming {
		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		}
	}

	reqOpts := mergeHeaders(model.Headers, opts.Headers)

	return ai.NewEventStream(func(push func(ai.Event)) {
		stream := p.client.Chat.Completions.NewStreaming(
			ctx,
			*params,
			reqOpts...,
		)

		var (
			contentIndex  int
			isActiveText  bool
			isActiveThink bool
			toolCalls     = make(map[int64]*streamToolCall)
			usage         ai.Usage
			stopReason    ai.StopReason
			textAccum     strings.Builder
			thinkAccum    strings.Builder
		)

		for stream.Next() {
			chunk := stream.Current()

			if chunk.Usage.TotalTokens > 0 {
				usage = mapUsage(chunk.Usage)
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			for _, choice := range chunk.Choices {
				if choice.FinishReason != "" {
					stopReason = mapStopReason(string(choice.FinishReason))
				}

				delta := choice.Delta

				reasoning := extractReasoning(delta)
				if reasoning != "" {
					if !isActiveThink {
						isActiveThink = true
						push(ai.Event{
							Type:         ai.EventThinkStart,
							ContentIndex: contentIndex,
						})
					}
					thinkAccum.WriteString(reasoning)
					push(ai.Event{
						Type:         ai.EventThinkDelta,
						ContentIndex: contentIndex,
						Delta:        reasoning,
					})
				}

				if delta.Content != "" {
					if isActiveThink {
						isActiveThink = false
						push(ai.Event{
							Type:         ai.EventThinkEnd,
							ContentIndex: contentIndex,
							Content:      thinkAccum.String(),
						})
						contentIndex++
					}
					if !isActiveText {
						isActiveText = true
						push(ai.Event{
							Type:         ai.EventTextStart,
							ContentIndex: contentIndex,
						})
					}
					textAccum.WriteString(delta.Content)
					push(ai.Event{
						Type:         ai.EventTextDelta,
						ContentIndex: contentIndex,
						Delta:        delta.Content,
					})
				}

				for _, tc := range delta.ToolCalls {
					existing, ok := toolCalls[tc.Index]
					if !ok {
						if isActiveText {
							isActiveText = false
							push(ai.Event{
								Type:         ai.EventTextEnd,
								ContentIndex: contentIndex,
								Content:      textAccum.String(),
							})
							contentIndex++
						}

						toolCalls[tc.Index] = &streamToolCall{
							id:   tc.ID,
							name: tc.Function.Name,
						}
						push(ai.Event{
							Type:         ai.EventToolStart,
							ContentIndex: contentIndex,
							ToolCall: &ai.ToolCall{
								ID:   tc.ID,
								Name: tc.Function.Name,
							},
						})
						if tc.Function.Arguments != "" {
							toolCalls[tc.Index].arguments.WriteString(tc.Function.Arguments)
							push(ai.Event{
								Type:         ai.EventToolDelta,
								ContentIndex: contentIndex,
								Delta:        tc.Function.Arguments,
							})
						}
					} else {
						if tc.Function.Arguments != "" {
							existing.arguments.WriteString(tc.Function.Arguments)
							push(ai.Event{
								Type:         ai.EventToolDelta,
								ContentIndex: contentIndex,
								Delta:        tc.Function.Arguments,
							})
						}
					}
				}
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

		if isActiveText {
			push(ai.Event{
				Type:         ai.EventTextEnd,
				ContentIndex: contentIndex,
				Content:      textAccum.String(),
			})
			contentIndex++
		}

		if isActiveThink {
			push(ai.Event{
				Type:         ai.EventThinkEnd,
				ContentIndex: contentIndex,
				Content:      thinkAccum.String(),
			})
			contentIndex++
		}

		for _, tc := range toolCalls {
			var args map[string]any
			if tc.arguments.Len() > 0 {
				_ = json.Unmarshal([]byte(tc.arguments.String()), &args)
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

		if len(toolCalls) > 0 {
			stopReason = ai.StopReasonToolUse
		}

		msg := buildFinalMessage(
			model,
			textAccum.String(),
			thinkAccum.String(),
			toolCalls,
			usage,
			stopReason,
		)

		log.Debug(
			"[OPENAI] completed",
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
	arguments strings.Builder
}

func buildParams(
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) *openai.ChatCompletionNewParams {
	compat := getCompat(model)

	params := &openai.ChatCompletionNewParams{
		Model: model.ID,
		Messages: convertMessages(
			prompt.System,
			prompt.Messages,
			compat,
		),
	}

	if compat.SupportsStore {
		params.Store = param.NewOpt(false)
	}

	if opts.MaxTokens != nil {
		if compat.MaxTokensField == "max_completion_tokens" {
			params.MaxCompletionTokens = param.NewOpt(int64(*opts.MaxTokens))
		} else {
			params.MaxTokens = param.NewOpt(int64(*opts.MaxTokens))
		}
	}

	if opts.Temperature != nil && compat.SupportsTemperature {
		params.Temperature = param.NewOpt(*opts.Temperature)
	}

	if opts.ThinkingLevel != "" && compat.SupportsReasoningEffort {
		params.ReasoningEffort = openai.ReasoningEffort(opts.ThinkingLevel)
	}

	if len(prompt.Tools) > 0 {
		params.Tools = convertTools(prompt.Tools, compat)
		if opts.ToolChoice != "" {
			tc := convertToolChoice(opts.ToolChoice)
			params.ToolChoice = tc
		}
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

	return &ai.Message{
		Role:       ai.RoleAssistant,
		Content:    content,
		API:        "openai-completions",
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      usage,
		StopReason: stopReason,
		Timestamp:  time.Now(),
	}
}

// extractReasoning pulls reasoning content from a chat completion delta.
// The OpenAI SDK doesn't have a typed field for this, so we check JSON extras.
func extractReasoning(delta openai.ChatCompletionChunkChoiceDelta) string {
	if f, ok := delta.JSON.ExtraFields["reasoning_content"]; ok {
		raw := f.Raw()
		if raw != "" && raw != "null" {
			var s string
			if err := json.Unmarshal([]byte(raw), &s); err == nil {
				return s
			}
		}
	}
	if f, ok := delta.JSON.ExtraFields["reasoning"]; ok {
		raw := f.Raw()
		if raw != "" && raw != "null" {
			var s string
			if err := json.Unmarshal([]byte(raw), &s); err == nil {
				return s
			}
		}
	}
	return ""
}

// GenerateObject generates a structured object using JSON mode.
func (p *Provider) GenerateObject(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	schema *jsonschema.Schema,
	opts ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	log.Debug(
		"[OPENAI] generating object",
		"model", model.ID,
		"messages", len(prompt.Messages),
	)

	params := buildParams(model, prompt, opts)

	params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONObject: &openai.ResponseFormatJSONObjectParam{},
	}

	reqOpts := mergeHeaders(model.Headers, opts.Headers)

	resp, err := p.client.Chat.Completions.New(ctx, *params, reqOpts...)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, errors.New("openai: no response generated")
	}

	raw := resp.Choices[0].Message.Content

	usage := mapUsage(resp.Usage)
	usage.Cost = ai.CalculateCost(model, usage)

	log.Debug(
		"[OPENAI] object completed",
		"model", model.ID,
		"input", usage.Input,
		"output", usage.Output,
	)

	return &ai.ObjectResponse{
		Raw:   raw,
		Usage: usage,
		Model: resp.Model,
	}, nil
}

// GenerateImage generates images using DALL-E.
func (p *Provider) GenerateImage(
	ctx context.Context,
	model ai.Model,
	req *ai.ImageRequest,
) (*ai.ImageResponse, error) {
	log.Debug(
		"[OPENAI] generating image",
		"model", model.ID,
		"prompt", req.Prompt,
	)

	modelID := model.ID
	if modelID == "" {
		modelID = "dall-e-3"
	}

	n := req.N
	if n <= 0 {
		n = 1
	}

	params := openai.ImageGenerateParams{
		Prompt:         req.Prompt,
		Model:          modelID,
		N:              param.NewOpt(int64(n)),
		ResponseFormat: openai.ImageGenerateParamsResponseFormatB64JSON,
	}

	if req.Size != "" {
		params.Size = openai.ImageGenerateParamsSize(req.Size)
	}

	resp, err := p.client.Images.Generate(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}

	images := make([]ai.GeneratedImage, len(resp.Data))
	for i, img := range resp.Data {
		data, err := base64.StdEncoding.DecodeString(img.B64JSON)
		if err != nil {
			return nil, fmt.Errorf("openai: failed to decode image: %w", err)
		}
		images[i] = ai.GeneratedImage{
			Data:      data,
			MediaType: "image/png",
		}
	}

	return &ai.ImageResponse{
		Images: images,
	}, nil
}
