package geminicli

import (
	"encoding/base64"
	"encoding/json"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// convertMessages converts ai messages to Gemini Content slices.
func convertMessages(messages []ai.Message) []Content {
	var contents []Content

	for _, msg := range messages {
		switch msg.Role {
		case ai.RoleUser:
			parts := convertUserParts(msg.Content)
			if len(parts) > 0 {
				contents = append(contents, Content{
					Role:  "user",
					Parts: parts,
				})
			}

		case ai.RoleAssistant:
			parts := convertAssistantParts(msg.Content)
			if len(parts) > 0 {
				contents = append(contents, Content{
					Role:  "model",
					Parts: parts,
				})
			}

		case ai.RoleToolResult:
			part := convertToolResult(msg)
			if part != nil {
				contents = append(contents, Content{
					Role:  "user",
					Parts: []*Part{part},
				})
			}
		}
	}

	return contents
}

// convertUserParts converts user message content to Gemini parts.
func convertUserParts(content []ai.Content) []*Part {
	var parts []*Part

	for _, c := range content {
		switch v := c.(type) {
		case ai.Text:
			if v.Text != "" {
				parts = append(parts, &Part{Text: v.Text})
			}

		case ai.Image:
			data, err := base64.StdEncoding.DecodeString(v.Data)
			if err != nil {
				continue
			}
			parts = append(parts, &Part{
				InlineData: &Blob{
					Data:     data,
					MIMEType: v.MimeType,
				},
			})

		case ai.File:
			if part := convertFile(v); part != nil {
				parts = append(parts, part)
			}
		}
	}

	return parts
}

// convertFile converts an ai.File to a Gemini Part. URL/FileID-based files
// use FileData; base64 Data uses InlineData. Unrecognized files are skipped.
func convertFile(f ai.File) *Part {
	switch {
	case f.URL != "":
		return &Part{
			FileData: &FileData{
				FileURI:  f.URL,
				MIMEType: f.MimeType,
			},
		}
	case f.FileID != "":
		return &Part{
			FileData: &FileData{
				FileURI:  f.FileID,
				MIMEType: f.MimeType,
			},
		}
	case f.Data != "":
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return nil
		}
		return &Part{
			InlineData: &Blob{
				Data:     data,
				MIMEType: f.MimeType,
			},
		}
	}
	return nil
}

// convertAssistantParts converts assistant message content to Gemini parts.
func convertAssistantParts(content []ai.Content) []*Part {
	var parts []*Part

	for _, c := range content {
		switch v := c.(type) {
		case ai.Thinking:
			p := &Part{
				Text:    v.Thinking,
				Thought: true,
			}
			if v.Signature != "" {
				p.ThoughtSignature = v.Signature
			}
			parts = append(parts, p)

		case ai.Text:
			p := &Part{Text: v.Text}
			if v.Signature != "" {
				p.ThoughtSignature = v.Signature
			}
			parts = append(parts, p)

		case ai.ToolCall:
			p := &Part{
				FunctionCall: &FunctionCall{
					ID:   v.ID,
					Name: v.Name,
					Args: v.Arguments,
				},
			}
			if v.Signature != "" {
				p.ThoughtSignature = v.Signature
			}
			parts = append(parts, p)
		}
	}

	return parts
}

// convertToolResult converts a tool result message to a Gemini function response.
func convertToolResult(msg ai.Message) *Part {
	var responseText string
	for _, c := range msg.Content {
		if t, ok := c.(ai.Text); ok {
			responseText = t.Text
			break
		}
	}

	var response map[string]any
	if msg.IsError {
		response = map[string]any{"error": responseText}
	} else {
		var parsed any
		if err := json.Unmarshal([]byte(responseText), &parsed); err == nil {
			response = map[string]any{"result": parsed}
		} else {
			response = map[string]any{"result": responseText}
		}
	}

	return &Part{
		FunctionResponse: &FunctionResponse{
			ID:       msg.ToolCallID,
			Name:     msg.ToolName,
			Response: response,
		},
	}
}

// convertTools converts ai.ToolInfo to Gemini tool declarations.
func convertTools(
	tools []ai.ToolInfo,
	choice ai.ToolChoice,
) ([]Tool, *ToolConfig) {
	var decls []FunctionDeclaration
	for _, t := range tools {
		var schemaMap map[string]any
		if t.InputSchema != nil {
			if data, err := json.Marshal(t.InputSchema); err == nil {
				_ = json.Unmarshal(data, &schemaMap)
			}
		}

		decls = append(decls, FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  schemaMap,
		})
	}

	var config *ToolConfig
	mode := convertToolChoice(choice)
	if mode != "" {
		config = &ToolConfig{
			FunctionCallingConfig: FunctionCallingConfig{Mode: mode},
		}
	}

	return []Tool{{FunctionDeclarations: decls}}, config
}

// convertToolChoice maps ai.ToolChoice to Gemini function calling mode.
func convertToolChoice(tc ai.ToolChoice) string {
	switch tc {
	case ai.ToolChoiceAuto, "":
		return "AUTO"
	case ai.ToolChoiceNone:
		return "NONE"
	case ai.ToolChoiceRequired:
		return "ANY"
	default:
		return "AUTO"
	}
}

// mapStopReason converts Gemini finish reason to ai.StopReason.
func mapStopReason(reason string) ai.StopReason {
	switch reason {
	case "STOP":
		return ai.StopReasonStop
	case "MAX_TOKENS":
		return ai.StopReasonLength
	default:
		return ai.StopReasonError
	}
}

// mapUsage converts Gemini usage metadata to ai.Usage.
func mapUsage(meta *UsageMetadata) ai.Usage {
	return ai.Usage{
		Input:     meta.PromptTokenCount - meta.CachedContentTokenCount,
		Output:    meta.CandidatesTokenCount,
		Total:     meta.TotalTokenCount,
		CacheRead: meta.CachedContentTokenCount,
	}
}
