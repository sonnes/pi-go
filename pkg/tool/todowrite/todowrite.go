// Package todowrite provides the TodoWrite tool: the agent emits the
// full updated checklist for the current session, and the tool
// validates it and acks. This is server-side validation and contract
// documentation only — there is no server-side store; a caller renders
// the checklist from the streamed tool_call arguments.
package todowrite

import (
	"context"
	"fmt"

	_ "embed"
)

// Description is the default tool documentation, embedded from
// description.md. Clients register the tool under any name and may pass
// this (or their own text) as the description.
//
//go:embed description.md
var Description string

// ToolName is the suggested default registry name for this tool.
const ToolName = "TodoWrite"

// Status is the lifecycle state of a single todo item.
type Status string

const (
	// StatusPending is a task not yet started.
	StatusPending Status = "pending"
	// StatusInProgress is the task currently being worked on. At most
	// one item may hold this status at a time.
	StatusInProgress Status = "in_progress"
	// StatusCompleted is a task finished successfully.
	StatusCompleted Status = "completed"
)

// Item is one entry in the checklist.
type Item struct {
	Content    string `json:"content"     jsonschema:"Imperative form describing the task, e.g. \"Run tests\". Required."`
	ActiveForm string `json:"active_form" jsonschema:"Present-continuous form shown while the task runs, e.g. \"Running tests\". Required."`
	Status     Status `json:"status"      jsonschema:"One of: pending, in_progress, completed."`
}

// Input is the tool's argument shape — the full updated list, which
// replaces the previous one entirely.
type Input struct {
	Todos []Item `json:"todos" jsonschema:"The full updated todo list. Replaces the previous list entirely. Keep exactly one item in_progress while work is ongoing."`
}

// todo validates checklist updates. It carries no dependencies — the
// tool is pure server-side validation, and identity (name, description)
// belongs to the client that registers it.
type todo struct{}

// Option configures a TodoWrite tool.
type Option interface{ apply(*todo) }

// ackMessage is returned on every successful update.
const ackMessage = "Todos have been modified successfully. Ensure that you continue to use the todo list to track your progress. Please proceed with the current tasks if applicable."

// shapeExample helps the model self-correct after a validation error.
const shapeExample = `Expected shape: {"todos":[{"content":"Run tests","active_form":"Running tests","status":"in_progress"},{"content":"Ship it","active_form":"Shipping it","status":"pending"}]}. Each item needs a non-empty content (imperative) and active_form (present continuous); status is one of pending, in_progress, completed; at most one item may be in_progress.`

// New returns the TodoWrite tool's runner: it validates the updated
// checklist and returns an acknowledgement. The tool is pure
// server-side validation; a caller renders the checklist from the
// streamed tool_call arguments. Pass the runner to ai.DefineTool with a
// name and description.
func New(opts ...Option) func(context.Context, Input) (string, error) {
	t := &todo{}
	for _, o := range opts {
		o.apply(t)
	}
	return t.run
}

func (t *todo) run(_ context.Context, input Input) (string, error) {
	if err := validate(input); err != nil {
		return "", fmt.Errorf("%s. %s", err, shapeExample)
	}
	return ackMessage, nil
}

func validate(in Input) error {
	inProgress := 0
	for i, todo := range in.Todos {
		if todo.Content == "" {
			return fmt.Errorf("todowrite: todo %d: content is required", i)
		}
		if todo.ActiveForm == "" {
			return fmt.Errorf("todowrite: todo %d: active_form is required", i)
		}
		switch todo.Status {
		case StatusPending, StatusInProgress, StatusCompleted:
		default:
			return fmt.Errorf("todowrite: todo %d: status must be one of pending, in_progress, completed", i)
		}
		if todo.Status == StatusInProgress {
			inProgress++
		}
	}
	if inProgress > 1 {
		return fmt.Errorf("todowrite: at most one todo may be in_progress at a time (found %d)", inProgress)
	}
	return nil
}
