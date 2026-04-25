package geminicli

// Request is the top-level request to the Cloud Code Assist API.
type Request struct {
	Model             string            `json:"model"`
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	Tools             []Tool            `json:"tools,omitempty"`
	ToolConfig        *ToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
}

// Content represents a message in Gemini format.
type Content struct {
	Role  string  `json:"role,omitempty"`
	Parts []*Part `json:"parts"`
}

// Part represents a content part.
type Part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
	InlineData       *Blob             `json:"inlineData,omitempty"`
	FileData         *FileData         `json:"fileData,omitempty"`
}

// FileData references a file by URI, used for documents and large media.
type FileData struct {
	FileURI  string `json:"fileUri"`
	MIMEType string `json:"mimeType,omitempty"`
}

// FunctionCall represents a model-initiated function invocation.
type FunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// FunctionResponse represents the result of a function call.
type FunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// Blob represents inline binary data.
type Blob struct {
	Data     []byte `json:"data"`
	MIMEType string `json:"mimeType"`
}

// Tool wraps function declarations.
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations"`
}

// FunctionDeclaration describes a callable function.
type FunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolConfig controls tool selection behavior.
type ToolConfig struct {
	FunctionCallingConfig FunctionCallingConfig `json:"functionCallingConfig"`
}

// FunctionCallingConfig controls function calling mode.
type FunctionCallingConfig struct {
	Mode string `json:"mode"` // AUTO, NONE, ANY
}

// GenerationConfig holds generation parameters.
type GenerationConfig struct {
	MaxOutputTokens *int32          `json:"maxOutputTokens,omitempty"`
	Temperature     *float32        `json:"temperature,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

// ThinkingConfig controls reasoning behavior.
type ThinkingConfig struct {
	IncludeThoughts bool   `json:"includeThoughts"`
	ThinkingBudget  *int32 `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"` // MINIMAL, LOW, MEDIUM, HIGH
}

// SSEChunk is the SSE response chunk from the Cloud Code Assist API.
// The API wraps the response in a "response" field envelope.
type SSEChunk struct {
	// Wrapped format: {"response": {"candidates": [...], "usageMetadata": {...}}}
	Response *SSEResponse `json:"response,omitempty"`

	// Unwrapped format: {"candidates": [...], "usageMetadata": {...}}
	Candidates    []Candidate    `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// SSEResponse is the inner response object in the wrapped SSE format.
type SSEResponse struct {
	Candidates    []Candidate    `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// GetCandidates returns candidates from either the wrapped or unwrapped format.
func (c *SSEChunk) GetCandidates() []Candidate {
	if c.Response != nil {
		return c.Response.Candidates
	}
	return c.Candidates
}

// GetUsageMetadata returns usage from either the wrapped or unwrapped format.
func (c *SSEChunk) GetUsageMetadata() *UsageMetadata {
	if c.Response != nil {
		return c.Response.UsageMetadata
	}
	return c.UsageMetadata
}

// Candidate represents a single model response candidate.
type Candidate struct {
	Content      *Content `json:"content,omitempty"`
	FinishReason string   `json:"finishReason,omitempty"`
}

// UsageMetadata contains token usage information.
type UsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
}
