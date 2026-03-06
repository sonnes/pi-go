package ai

// Content is a sealed interface for content blocks within messages.
// The concrete types are Text, Thinking, Image, and ToolCall.
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
