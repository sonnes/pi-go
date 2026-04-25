package google

import (
	"encoding/base64"
	"encoding/json"

	ai "github.com/sonnes/pi-go/pkg/ai"

	"google.golang.org/genai"
)

// convertMessages converts messages to Google genai Content slices.
func convertMessages(messages []ai.Message) []*genai.Content {
	var contents []*genai.Content

	for _, msg := range messages {
		switch msg.Role {
		case ai.RoleUser:
			parts := convertUserParts(msg.Content)
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{
					Role:  genai.RoleUser,
					Parts: parts,
				})
			}

		case ai.RoleAssistant:
			parts := convertAssistantParts(msg.Content)
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{
					Role:  genai.RoleModel,
					Parts: parts,
				})
			}

		case ai.RoleToolResult:
			part := convertToolResult(msg)
			if part != nil {
				contents = append(contents, &genai.Content{
					Role:  genai.RoleUser,
					Parts: []*genai.Part{part},
				})
			}
		}
	}

	return contents
}

// convertUserParts converts user message content to Google parts.
func convertUserParts(content []ai.Content) []*genai.Part {
	var parts []*genai.Part

	for _, c := range content {
		switch v := c.(type) {
		case ai.Text:
			if v.Text != "" {
				parts = append(parts, &genai.Part{Text: v.Text})
			}

		case ai.Image:
			data, err := base64.StdEncoding.DecodeString(v.Data)
			if err != nil {
				continue
			}
			parts = append(parts, &genai.Part{
				InlineData: &genai.Blob{
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

// convertFile converts an ai.File to a Google genai Part. URL/FileID-based
// files use FileData; base64 Data uses InlineData. Unrecognized files (no
// data, URL, or FileID) are skipped.
func convertFile(f ai.File) *genai.Part {
	switch {
	case f.URL != "":
		return &genai.Part{
			FileData: &genai.FileData{
				FileURI:     f.URL,
				MIMEType:    f.MimeType,
				DisplayName: f.Filename,
			},
		}
	case f.FileID != "":
		return &genai.Part{
			FileData: &genai.FileData{
				FileURI:     f.FileID,
				MIMEType:    f.MimeType,
				DisplayName: f.Filename,
			},
		}
	case f.Data != "":
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return nil
		}
		return &genai.Part{
			InlineData: &genai.Blob{
				Data:     data,
				MIMEType: f.MimeType,
			},
		}
	}
	return nil
}

// convertAssistantParts converts assistant message content to Google parts.
func convertAssistantParts(content []ai.Content) []*genai.Part {
	var parts []*genai.Part

	for _, c := range content {
		switch v := c.(type) {
		case ai.Thinking:
			gPart := &genai.Part{
				Text:    v.Thinking,
				Thought: true,
			}
			if v.Signature != "" {
				gPart.ThoughtSignature = []byte(v.Signature)
			}
			parts = append(parts, gPart)

		case ai.Text:
			gPart := &genai.Part{Text: v.Text}
			if v.Signature != "" {
				gPart.ThoughtSignature = []byte(v.Signature)
			}
			parts = append(parts, gPart)

		case ai.ToolCall:
			gPart := &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   v.ID,
					Name: v.Name,
					Args: v.Arguments,
				},
			}
			if v.Signature != "" {
				gPart.ThoughtSignature = []byte(v.Signature)
			}
			parts = append(parts, gPart)
		}
	}

	return parts
}

// convertToolResult converts a tool result message to a Google function response part.
func convertToolResult(msg ai.Message) *genai.Part {
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

	return &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			ID:       msg.ToolCallID,
			Name:     msg.ToolName,
			Response: response,
		},
	}
}
