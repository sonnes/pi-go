// Package prompt provides composable system prompt building.
//
// A [Prompt] is an ordered slice of [Section] values. Each section
// contributes a piece of the system prompt, identified by a unique key.
// The agent package concatenates sections with double newlines before
// each LLM call.
//
// Applications implement [Section] to define their own prompt pieces:
//
//	type RoleSection struct{}
//
//	func (RoleSection) Key() string     { return "role" }
//	func (RoleSection) Content() string { return "You are a helpful assistant." }
package prompt
