package ai

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

// Thinking represents reasoning/chain-of-thought content.
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

// File represents a document or file attachment provided by the user.
// Exactly one of Data, URL, or FileID should be set:
//   - Data is base64-encoded file content (inline upload).
//   - URL is a publicly accessible URL the provider can fetch.
//   - FileID is a provider-specific identifier for a previously uploaded file.
//
// MimeType is the IANA media type (e.g. "application/pdf", "text/plain").
// Filename is an optional human-readable name surfaced by some providers.
type File struct {
	Data     string // base64 encoded
	URL      string
	FileID   string
	MimeType string
	Filename string
}

func (File) content() {}

// ToolCall represents a tool invocation by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
	Signature string // provider-specific signature (e.g. Google thought_signature)
}

func (ToolCall) content() {}

// AsContent converts a Content interface to a specific concrete type.
func AsContent[T Content](c Content) (T, bool) {
	var zero T
	if c == nil {
		return zero, false
	}
	v, ok := c.(T)
	return v, ok
}
