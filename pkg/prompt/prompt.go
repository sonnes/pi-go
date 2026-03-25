package prompt

// Section is a composable piece of a system prompt. Implementations
// provide a unique key for identification and rendered content.
type Section interface {
	// Key returns a unique identifier for this section.
	Key() string

	// Content returns the rendered text for this section.
	Content() string
}

// Prompt is an ordered collection of [Section] values that are
// concatenated to form a system prompt.
type Prompt []Section
