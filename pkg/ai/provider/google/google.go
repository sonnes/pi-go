// Package google provides a Google AI (Gemini) provider for the AI SDK.
package google

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"google.golang.org/genai"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/oauth"
)

// Verify interface compliance.
var (
	_ ai.Provider       = (*Provider)(nil)
	_ ai.ObjectProvider = (*Provider)(nil)
	_ ai.ImageProvider  = (*Provider)(nil)
)

// Provider implements the Google AI (Gemini) provider.
type Provider struct {
	client         *genai.Client
	apiKey         string
	httpClient     *http.Client
	toolCallIDFunc func() string
}

// Option configures the Google provider.
type Option func(*Provider)

// WithAPIKey sets the API key for authentication.
func WithAPIKey(key string) Option {
	return func(p *Provider) { p.apiKey = key }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) Option {
	return func(p *Provider) { p.httpClient = c }
}

// WithToolCallIDFunc sets a custom function for generating tool call IDs.
func WithToolCallIDFunc(fn func() string) Option {
	return func(p *Provider) { p.toolCallIDFunc = fn }
}

// WithOAuth configures the provider for OAuth Bearer token authentication.
// It sets up automatic token refresh via the [oauth.Transport] middleware.
// Additional transport options (e.g. [oauth.WithOnRefresh] for credential
// persistence) can be passed.
func WithOAuth(clientID, clientSecret string, creds oauth.Credentials, opts ...oauth.TransportOption) Option {
	return func(p *Provider) {
		transport := NewOAuthTransport(clientID, clientSecret, creds, opts...)
		p.httpClient = &http.Client{Transport: transport}
	}
}

// New creates a new Google AI provider.
func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		toolCallIDFunc: func() string {
			return uuid.NewString()
		},
	}
	for _, opt := range opts {
		opt(p)
	}

	cc := &genai.ClientConfig{
		HTTPClient: p.httpClient,
		Backend:    genai.BackendGeminiAPI,
		APIKey:     p.apiKey,
	}

	client, err := genai.NewClient(context.Background(), cc)
	if err != nil {
		return nil, fmt.Errorf("google: failed to create client: %w", err)
	}

	p.client = client
	return p, nil
}

// API returns the provider API identifier.
func (p *Provider) API() string {
	return "google-generative"
}

// StreamText streams a text response from the model.
func (p *Provider) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) *ai.EventStream {
	log.Debug("[GOOGLE] starting stream",
		"model", model.ID,
		"messages", len(prompt.Messages),
		"tools", len(prompt.Tools),
	)

	return ai.NewEventStream(func(push func(ai.Event)) {
		config := &genai.GenerateContentConfig{}

		if prompt.System != "" {
			config.SystemInstruction = &genai.Content{
				Parts: []*genai.Part{{Text: prompt.System}},
			}
		}

		contents := convertMessages(prompt.Messages)
		if len(contents) == 0 {
			push(ai.Event{
				Type: ai.EventError,
				Err:  errors.New("google: no messages to send"),
			})
			return
		}

		applyOptions(config, opts)

		if len(prompt.Tools) > 0 {
			config.Tools, config.ToolConfig = convertTools(prompt.Tools, opts.ToolChoice)
		}

		if len(model.Headers) > 0 {
			headers := http.Header{}
			for k, v := range model.Headers {
				headers.Add(k, v)
			}
			config.HTTPOptions = &genai.HTTPOptions{
				Headers: headers,
			}
		}

		lastMessage := contents[len(contents)-1]
		history := contents[:len(contents)-1]

		chat, err := p.client.Chats.Create(ctx, model.ID, config, history)
		if err != nil {
			push(ai.Event{
				Type: ai.EventError,
				Err:  fmt.Errorf("google: %w", err),
			})
			return
		}

		lastParts := depointerSlice(lastMessage.Parts)

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

		for resp, err := range chat.SendMessageStream(ctx, lastParts...) {
			if err != nil {
				push(ai.Event{
					Type: ai.EventError,
					Err:  fmt.Errorf("google: %w", err),
				})
				return
			}

			if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
				for _, part := range resp.Candidates[0].Content.Parts {
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
							sig := ""
							if part.ThoughtSignature != nil {
								sig = string(part.ThoughtSignature)
							}
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
						toolCallID := cmp.Or(part.FunctionCall.ID, p.toolCallIDFunc())
						args := part.FunctionCall.Args

						tc := &ai.ToolCall{
							ID:        toolCallID,
							Name:      part.FunctionCall.Name,
							Arguments: args,
						}
						if part.ThoughtSignature != nil {
							tc.Signature = string(part.ThoughtSignature)
						}

						if inText {
							sig := ""
							if part.ThoughtSignature != nil {
								sig = string(part.ThoughtSignature)
							}
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
							sig := ""
							if part.ThoughtSignature != nil {
								sig = string(part.ThoughtSignature)
							}
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

						argsJSON, _ := json.Marshal(args)

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

			if resp.UsageMetadata != nil && resp.UsageMetadata.TotalTokenCount > 0 {
				usage = mapUsage(resp.UsageMetadata)
			}

			if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason != "" {
				stopReason = mapStopReason(resp.Candidates[0].FinishReason)
			}
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

		finalMessage := &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    finalContent,
			Usage:      usage,
			StopReason: stopReason,
		}

		log.Debug("[GOOGLE] stream completed",
			"model", model.ID,
			"stop_reason", stopReason,
			"input", usage.Input,
			"output", usage.Output,
		)

		push(ai.Event{
			Type:       ai.EventDone,
			Message:    finalMessage,
			StopReason: stopReason,
		})
	})
}

// applyOptions maps StreamOptions to genai config.
func applyOptions(config *genai.GenerateContentConfig, opts ai.StreamOptions) {
	if opts.Temperature != nil {
		tmp := float32(*opts.Temperature)
		config.Temperature = &tmp
	}
	if opts.MaxTokens != nil {
		config.MaxOutputTokens = int32(*opts.MaxTokens)
	}
	if opts.ThinkingLevel != "" {
		config.ThinkingConfig = &genai.ThinkingConfig{
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
	}
}

// convertTools converts ai.ToolInfo definitions to Google format.
func convertTools(tools []ai.ToolInfo, choice ai.ToolChoice) ([]*genai.Tool, *genai.ToolConfig) {
	var funcs []*genai.FunctionDeclaration
	for _, t := range tools {
		decl := &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
		}
		if t.InputSchema != nil {
			if data, err := json.Marshal(t.InputSchema); err == nil {
				var schemaMap map[string]any
				if err := json.Unmarshal(data, &schemaMap); err == nil {
					decl.Parameters = mapToGenaiSchema(schemaMap)
				}
			}
		}
		funcs = append(funcs, decl)
	}

	var googleTools []*genai.Tool
	if len(funcs) > 0 {
		googleTools = []*genai.Tool{{FunctionDeclarations: funcs}}
	}

	var toolConfig *genai.ToolConfig
	switch choice {
	case ai.ToolChoiceAuto, "":
		toolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		}
	case ai.ToolChoiceNone:
		toolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeNone,
			},
		}
	case ai.ToolChoiceRequired:
		toolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAny,
			},
		}
	default:
		toolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode:                 genai.FunctionCallingConfigModeAny,
				AllowedFunctionNames: []string{string(choice)},
			},
		}
	}

	return googleTools, toolConfig
}

// mapToGenaiSchema converts a JSON schema map to Google's genai.Schema.
func mapToGenaiSchema(m map[string]any) *genai.Schema {
	if m == nil {
		return nil
	}

	schema := &genai.Schema{}

	if typeVal, ok := m["type"].(string); ok {
		switch typeVal {
		case "string":
			schema.Type = genai.TypeString
		case "number":
			schema.Type = genai.TypeNumber
		case "integer":
			schema.Type = genai.TypeInteger
		case "boolean":
			schema.Type = genai.TypeBoolean
		case "array":
			schema.Type = genai.TypeArray
		case "object":
			schema.Type = genai.TypeObject
		default:
			schema.Type = genai.TypeString
		}
	}

	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}

	if props, ok := m["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for name, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				schema.Properties[name] = mapToGenaiSchema(propMap)
			}
		}
	}

	if required, ok := m["required"].([]any); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	if items, ok := m["items"].(map[string]any); ok {
		schema.Items = mapToGenaiSchema(items)
	}

	if enum, ok := m["enum"].([]any); ok {
		for _, e := range enum {
			if s, ok := e.(string); ok {
				schema.Enum = append(schema.Enum, s)
			}
		}
	}

	return schema
}

// mapUsage converts Google usage metadata to Usage.
func mapUsage(meta *genai.GenerateContentResponseUsageMetadata) ai.Usage {
	if meta == nil {
		return ai.Usage{}
	}
	return ai.Usage{
		Input:     int(meta.PromptTokenCount),
		Output:    int(meta.CandidatesTokenCount),
		Total:     int(meta.TotalTokenCount),
		CacheRead: int(meta.CachedContentTokenCount),
	}
}

// mapStopReason converts Google finish reason to StopReason.
func mapStopReason(reason genai.FinishReason) ai.StopReason {
	switch reason {
	case genai.FinishReasonStop:
		return ai.StopReasonStop
	case genai.FinishReasonMaxTokens:
		return ai.StopReasonLength
	case genai.FinishReasonSafety,
		genai.FinishReasonBlocklist,
		genai.FinishReasonProhibitedContent,
		genai.FinishReasonSPII,
		genai.FinishReasonImageSafety:
		return ai.StopReasonError
	default:
		return ai.StopReasonStop
	}
}

// depointerSlice converts a slice of pointers to a slice of values.
func depointerSlice[T any](s []*T) []T {
	result := make([]T, 0, len(s))
	for _, v := range s {
		if v != nil {
			result = append(result, *v)
		}
	}
	return result
}

// GenerateObject generates a structured object using native JSON schema mode.
func (p *Provider) GenerateObject(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	schema *jsonschema.Schema,
	opts ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	log.Debug(
		"[GOOGLE] generating object",
		"model", model.ID,
		"messages", len(prompt.Messages),
	)

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
	}

	if schema != nil {
		schemaMap, err := schemaToMapAny(schema)
		if err != nil {
			return nil, fmt.Errorf("google: %w", err)
		}
		config.ResponseJsonSchema = schemaMap
	}

	if prompt.System != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: prompt.System}},
		}
	}

	applyOptions(config, opts)

	contents := convertMessages(prompt.Messages)
	if len(contents) == 0 {
		return nil, errors.New("google: no messages to send")
	}

	lastMessage := contents[len(contents)-1]
	history := contents[:len(contents)-1]

	chat, err := p.client.Chats.Create(ctx, model.ID, config, history)
	if err != nil {
		return nil, fmt.Errorf("google: %w", err)
	}

	lastParts := depointerSlice(lastMessage.Parts)

	response, err := chat.SendMessage(ctx, lastParts...)
	if err != nil {
		return nil, fmt.Errorf("google: %w", err)
	}

	if len(response.Candidates) == 0 || response.Candidates[0].Content == nil {
		return nil, errors.New("google: no response from model")
	}

	var raw string
	for _, part := range response.Candidates[0].Content.Parts {
		if part.Text != "" && !part.Thought {
			raw += part.Text
		}
	}

	usage := mapUsage(response.UsageMetadata)
	usage.Cost = ai.CalculateCost(model, usage)

	log.Debug(
		"[GOOGLE] object completed",
		"model", model.ID,
		"input", usage.Input,
		"output", usage.Output,
	)

	return &ai.ObjectResponse{
		Raw:   raw,
		Usage: usage,
		Model: model.ID,
	}, nil
}

// GenerateImage generates images using Imagen models.
func (p *Provider) GenerateImage(
	ctx context.Context,
	model ai.Model,
	req *ai.ImageRequest,
) (*ai.ImageResponse, error) {
	log.Debug(
		"[GOOGLE] generating image",
		"model", model.ID,
		"prompt", req.Prompt,
	)

	modelID := cmp.Or(model.ID, "imagen-3.0-generate-002")

	n := req.N
	if n <= 0 {
		n = 1
	}

	config := &genai.GenerateImagesConfig{
		NumberOfImages: int32(n),
	}

	if req.Size != "" {
		config.AspectRatio = sizeToAspectRatio(req.Size)
	}

	response, err := p.client.Models.GenerateImages(
		ctx,
		modelID,
		req.Prompt,
		config,
	)
	if err != nil {
		return nil, fmt.Errorf("google: %w", err)
	}

	images := make([]ai.GeneratedImage, 0, len(response.GeneratedImages))
	for _, img := range response.GeneratedImages {
		if img.Image != nil {
			images = append(images, ai.GeneratedImage{
				Data:      img.Image.ImageBytes,
				MediaType: img.Image.MIMEType,
			})
		}
	}

	return &ai.ImageResponse{
		Images: images,
	}, nil
}

// sizeToAspectRatio converts a size string like "1024x1024" to an aspect ratio.
func sizeToAspectRatio(size string) string {
	switch size {
	case "1024x1024", "512x512", "256x256":
		return "1:1"
	case "1024x768", "768x1024":
		return "4:3"
	case "1280x720", "1920x1080":
		return "16:9"
	case "720x1280", "1080x1920":
		return "9:16"
	default:
		return "1:1"
	}
}

// schemaToMapAny converts a jsonschema.Schema to map[string]any for the Google API.
func schemaToMapAny(schema *jsonschema.Schema) (map[string]any, error) {
	if schema == nil {
		return nil, nil
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(data, &schemaMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	return schemaMap, nil
}
