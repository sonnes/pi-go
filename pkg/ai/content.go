package ai

import "encoding/json"

// Content is a sealed interface for content blocks within messages.
// The concrete types are Text, Thinking, Image, File, and ToolCall.
type Content interface {
	content()
}

// Text represents text content.
type Text struct {
	Text      string
	Signature string // provider-specific signature (OpenAI, Google)
}

func (Text) content() {}

// Thinking represents chain-of-thought reasoning content.
type Thinking struct {
	Thinking  string
	Signature string // provider-specific thought signature
}

func (Thinking) content() {}

// Image represents base64-encoded image content.
type Image struct {
	Data     string // base64 encoded
	MimeType string
}

func (Image) content() {}

// File represents a document or file attachment from the user.
// Set exactly one of Data, URL, or FileID:
//   - Data is base64-encoded file content (inline upload).
//   - URL is a public URL that the provider can fetch.
//   - FileID is a provider-specific identifier for a file you uploaded
//     earlier.
//
// MimeType is the IANA media type, for example "application/pdf" or
// "text/plain". Filename is an optional human-readable name. Some providers
// show it.
type File struct {
	Data     string // base64 encoded
	URL      string
	FileID   string
	MimeType string
	Filename string
}

func (File) content() {}

// ToolCall represents a tool invocation by the model.
//
// For client-side function tools, the model emits a ToolCall and the client
// runs it. The client then sends the result back in a separate tool-result
// message.
//
// For provider-hosted server tools, the provider runs the call inline. Server
// is true, and ServerType identifies the canonical tool. If Output is
// populated, it carries the provider result next to the invocation.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
	Signature string // provider-specific signature (Google thought_signature)

	Server     bool              // true when the provider ran this call
	ServerType ServerToolType    // canonical type when Server is true
	Output     *ServerToolOutput // result from the provider, when available
}

func (ToolCall) content() {}

// ServerToolOutput carries the result of a server tool that the provider runs.
//
// Content is normalized text. It can be concatenated search-result snippets,
// the stdout and stderr of code, or a fetched body. You can show this text or
// send it back to the model. Raw keeps the original JSON from the provider,
// for callers that need structured fields such as citations and encrypted
// indices.
type ServerToolOutput struct {
	Content string
	Raw     json.RawMessage
	IsError bool
}

// AsContent converts a Content interface to a specific concrete type.
func AsContent[T Content](c Content) (T, bool) {
	var zero T
	if c == nil {
		return zero, false
	}
	v, ok := c.(T)
	return v, ok
}
