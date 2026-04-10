// Package geminicli provides a Google Gemini CLI (Cloud Code Assist) provider.
package geminicli

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

const (
	defaultEndpoint = "https://cloudcode-pa.googleapis.com"
	streamPath      = "/v1internal:streamGenerateContent?alt=sse"
)

// Credentials holds the OAuth token and project ID for authentication.
type Credentials struct {
	Token     string `json:"token"`
	ProjectID string `json:"projectId"`
}

// Provider implements [ai.Provider] for the Cloud Code Assist API.
type Provider struct {
	creds          Credentials
	httpClient     *http.Client
	endpoint       string
	toolCallIDFunc func() string
}

// Verify interface compliance.
var _ ai.Provider = (*Provider)(nil)

// Option configures a [Provider].
type Option func(*Provider)

// WithCredentials sets the authentication credentials.
func WithCredentials(c Credentials) Option {
	return func(p *Provider) { p.creds = c }
}

// WithHTTPClient sets the HTTP client for requests.
func WithHTTPClient(c *http.Client) Option {
	return func(p *Provider) { p.httpClient = c }
}

// WithEndpoint sets the API endpoint URL.
func WithEndpoint(url string) Option {
	return func(p *Provider) { p.endpoint = url }
}

// WithToolCallIDFunc sets the function used to generate tool call IDs.
func WithToolCallIDFunc(fn func() string) Option {
	return func(p *Provider) { p.toolCallIDFunc = fn }
}

// New creates a new Gemini CLI provider.
func New(opts ...Option) *Provider {
	p := &Provider{
		httpClient:     http.DefaultClient,
		endpoint:       defaultEndpoint,
		toolCallIDFunc: uuid.NewString,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// API returns the provider API identifier.
func (p *Provider) API() string {
	return "google-gemini-cli"
}

// StreamText streams a text response from the Cloud Code Assist API.
func (p *Provider) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) *ai.EventStream {
	log.Debug(
		"[GEMINI-CLI] starting stream",
		"model", model.ID,
		"messages", len(prompt.Messages),
		"tools", len(prompt.Tools),
	)

	return ai.NewEventStream(func(push func(ai.Event)) {
		req, err := p.buildRequest(ctx, model, prompt, opts)
		if err != nil {
			push(ai.Event{Type: ai.EventError, Err: err})
			return
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			push(ai.Event{
				Type: ai.EventError,
				Err:  fmt.Errorf("geminicli: %w", err),
			})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			push(ai.Event{
				Type: ai.EventError,
				Err: fmt.Errorf(
					"geminicli: HTTP %d: %s",
					resp.StatusCode,
					string(body),
				),
			})
			return
		}

		p.processSSE(resp.Body, push, model)
	})
}

func (p *Provider) buildRequest(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) (*http.Request, error) {
	apiReq := Request{
		Model:    model.ID,
		Contents: convertMessages(prompt.Messages),
	}

	if prompt.System != "" {
		apiReq.SystemInstruction = &Content{
			Parts: []*Part{{Text: prompt.System}},
		}
	}

	apiReq.GenerationConfig = buildGenerationConfig(opts)

	if len(prompt.Tools) > 0 {
		apiReq.Tools, apiReq.ToolConfig = convertTools(
			prompt.Tools,
			opts.ToolChoice,
		)
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("geminicli: marshal: %w", err)
	}

	url := p.endpoint + streamPath
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("geminicli: request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.creds.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.creds.Token)
	}
	req.Header.Set("User-Agent", "cloud-code-assist/pi-go")

	for k, v := range model.Headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

func buildGenerationConfig(opts ai.StreamOptions) *GenerationConfig {
	config := &GenerationConfig{}
	hasConfig := false

	if opts.Temperature != nil {
		tmp := float32(*opts.Temperature)
		config.Temperature = &tmp
		hasConfig = true
	}

	if opts.MaxTokens != nil {
		tmp := int32(*opts.MaxTokens)
		config.MaxOutputTokens = &tmp
		hasConfig = true
	}

	if opts.ThinkingLevel != "" {
		config.ThinkingConfig = &ThinkingConfig{
			IncludeThoughts: true,
		}

		var budget int32
		switch opts.ThinkingLevel {
		case ai.ThinkingMinimal:
			budget = 128
		case ai.ThinkingLow:
			budget = 2048
		case ai.ThinkingMedium:
			budget = 8192
		case ai.ThinkingHigh:
			budget = 24576
		case ai.ThinkingXHigh:
			budget = 32768
		}
		if budget > 0 {
			config.ThinkingConfig.ThinkingBudget = &budget
		}
		hasConfig = true
	}

	if !hasConfig {
		return nil
	}
	return config
}

func (p *Provider) processSSE(
	body io.Reader,
	push func(ai.Event),
	model ai.Model,
) {
	var (
		contentIndex int
		inText       bool
		inThink      bool
		fullText     string
		fullThinking string
		hasToolCalls bool
		finalContent []ai.Content
		usage        ai.Usage
		stopReason   ai.StopReason
	)

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}

		var chunk SSEChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		candidates := chunk.GetCandidates()
		if len(candidates) > 0 && candidates[0].Content != nil {
			for _, part := range candidates[0].Content.Parts {
				switch {
				case part.Thought && part.Text != "":
					if !inThink {
						inThink = true
						push(ai.Event{
							Type:         ai.EventThinkStart,
							ContentIndex: contentIndex,
						})
					}
					fullThinking += part.Text
					push(ai.Event{
						Type:         ai.EventThinkDelta,
						ContentIndex: contentIndex,
						Delta:        part.Text,
					})

				case part.Text != "":
					if inThink {
						sig := part.ThoughtSignature
						finalContent = append(finalContent, ai.Thinking{
							Thinking:  fullThinking,
							Signature: sig,
						})
						push(ai.Event{
							Type:         ai.EventThinkEnd,
							ContentIndex: contentIndex,
							Content:      fullThinking,
						})
						inThink = false
						fullThinking = ""
						contentIndex++
					}

					if !inText {
						inText = true
						push(ai.Event{
							Type:         ai.EventTextStart,
							ContentIndex: contentIndex,
						})
					}
					fullText += part.Text
					push(ai.Event{
						Type:         ai.EventTextDelta,
						ContentIndex: contentIndex,
						Delta:        part.Text,
					})

				case part.FunctionCall != nil:
					hasToolCalls = true
					toolCallID := cmp.Or(
						part.FunctionCall.ID,
						p.toolCallIDFunc(),
					)

					tc := &ai.ToolCall{
						ID:        toolCallID,
						Name:      part.FunctionCall.Name,
						Arguments: part.FunctionCall.Args,
					}
					if part.ThoughtSignature != "" {
						tc.Signature = part.ThoughtSignature
					}

					if inText {
						sig := part.ThoughtSignature
						finalContent = append(finalContent, ai.Text{
							Text:      fullText,
							Signature: sig,
						})
						push(ai.Event{
							Type:         ai.EventTextEnd,
							ContentIndex: contentIndex,
							Content:      fullText,
						})
						inText = false
						fullText = ""
						contentIndex++
					}

					if inThink {
						sig := part.ThoughtSignature
						finalContent = append(finalContent, ai.Thinking{
							Thinking:  fullThinking,
							Signature: sig,
						})
						push(ai.Event{
							Type:         ai.EventThinkEnd,
							ContentIndex: contentIndex,
							Content:      fullThinking,
						})
						inThink = false
						fullThinking = ""
						contentIndex++
					}

					argsJSON, _ := json.Marshal(tc.Arguments)

					push(ai.Event{
						Type:         ai.EventToolStart,
						ContentIndex: contentIndex,
						ToolCall:     tc,
					})
					push(ai.Event{
						Type:         ai.EventToolDelta,
						ContentIndex: contentIndex,
						Delta:        string(argsJSON),
					})
					push(ai.Event{
						Type:         ai.EventToolEnd,
						ContentIndex: contentIndex,
						ToolCall:     tc,
					})

					finalContent = append(finalContent, *tc)
					contentIndex++
				}
			}
		}

		usageMeta := chunk.GetUsageMetadata()
		if usageMeta != nil && usageMeta.TotalTokenCount > 0 {
			usage = mapUsage(usageMeta)
		}

		if len(candidates) > 0 && candidates[0].FinishReason != "" {
			stopReason = mapStopReason(candidates[0].FinishReason)
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		push(ai.Event{
			Type: ai.EventError,
			Err:  fmt.Errorf("geminicli: stream read: %w", err),
		})
		return
	}

	if inThink {
		finalContent = append(finalContent, ai.Thinking{
			Thinking: fullThinking,
		})
		push(ai.Event{
			Type:         ai.EventThinkEnd,
			ContentIndex: contentIndex,
			Content:      fullThinking,
		})
		contentIndex++
	}
	if inText {
		finalContent = append(finalContent, ai.Text{Text: fullText})
		push(ai.Event{
			Type:         ai.EventTextEnd,
			ContentIndex: contentIndex,
			Content:      fullText,
		})
		contentIndex++
	}

	if hasToolCalls {
		stopReason = ai.StopReasonToolUse
	} else if stopReason == "" {
		stopReason = ai.StopReasonStop
	}

	usage.Cost = ai.CalculateCost(model, usage)

	finalMessage := &ai.Message{
		Role:       ai.RoleAssistant,
		Content:    finalContent,
		API:        "google-gemini-cli",
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      usage,
		StopReason: stopReason,
		Timestamp:  time.Now(),
	}

	log.Debug(
		"[GEMINI-CLI] completed",
		"model", model.ID,
		"stop", stopReason,
		"input", usage.Input,
		"output", usage.Output,
	)

	push(ai.Event{
		Type:       ai.EventDone,
		Message:    finalMessage,
		StopReason: stopReason,
	})
}
