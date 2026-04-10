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

// Provider implements [ai.Provider] for OpenAI's Responses API.
type Provider struct {
	client *openai.Client
}

// Verify interface compliance.
var _ ai.Provider = (*Provider)(nil)

// New creates a new OpenAI Responses provider.
func New(opts ...option.RequestOption) *Provider {
	opts = append(
		[]option.RequestOption{option.WithMaxRetries(0)},
		opts...,
	)
	client := openai.NewClient(opts...)
	return &Provider{client: &client}
}

// API returns the provider API identifier.
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

	params := buildParams(model, prompt, opts)
	reqOpts := mergeHeaders(model.Headers, opts.Headers)

	return ai.NewEventStream(func(push func(ai.Event)) {
		stream := p.client.Responses.NewStreaming(
			ctx,
			*params,
			reqOpts...,
		)

		var (
			contentIndex int
			inText       bool
			inThink      bool
			toolCalls    = make(map[int64]*streamToolCall)
			usage        ai.Usage
			stopReason   ai.StopReason
			textAccum    strings.Builder
			thinkAccum   strings.Builder
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

			case "response.output_item.added":
				if event.Item.Type == "function_call" {
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
		params.Tools = convertTools(prompt.Tools)
		if opts.ToolChoice != "" {
			params.ToolChoice = convertToolChoice(opts.ToolChoice)
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
		API:        "openai-responses",
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      usage,
		StopReason: stopReason,
		Timestamp:  time.Now(),
	}
}
