package agent

import (
	"context"

	"github.com/sonnes/pi-go/pkg/ai"
)

// TurnResult is the public view of a completed turn, passed to hooks.
type TurnResult struct {
	AssistantMsg ai.Message
	ToolResults  []ai.Message
	Usage        ai.Usage
}

// Hooks configure lifecycle callbacks for the agent loop.
// All hooks are optional — nil hooks are skipped.
type Hooks struct {
	// TransformMessages converts agent [Message] values to [ai.Message]
	// values before each LLM call. This replaces the default [LLMMessages]
	// conversion, giving extensions full control over what the model sees —
	// including the ability to inspect or filter [CustomMessage] values.
	//
	// When nil, [LLMMessages] is used as the default.
	TransformMessages func(ctx context.Context, msgs []Message) []ai.Message

	// AfterTurn is called after each turn completes. It receives the
	// current conversation messages and the turn's result.
	//
	// If it returns a non-nil slice, that slice replaces the agent's
	// message history (e.g. for compaction). Returning nil keeps the
	// history unchanged.
	AfterTurn func(ctx context.Context, msgs []Message, tr TurnResult) []Message

	// FollowUp is called when the agent would stop (no tool calls).
	// If it returns a non-nil, non-empty slice, the messages are appended
	// to the history and the loop continues for another turn.
	//
	// Returning nil or an empty slice lets the agent stop normally.
	// [FollowUp] is not called after turns that made tool calls, since
	// the loop already continues in that case.
	FollowUp func(ctx context.Context, msgs []Message) []Message
}

// transformMessages calls [Hooks.TransformMessages] if set, otherwise
// falls back to [LLMMessages].
func (h *Hooks) transformMessages(ctx context.Context, msgs []Message) []ai.Message {
	if h.TransformMessages != nil {
		return h.TransformMessages(ctx, msgs)
	}
	return LLMMessages(msgs)
}

// afterTurn calls [Hooks.AfterTurn] if set. Returns the replacement
// messages, or nil if the history should not change.
func (h *Hooks) afterTurn(ctx context.Context, msgs []Message, tr TurnResult) []Message {
	if h.AfterTurn != nil {
		return h.AfterTurn(ctx, msgs, tr)
	}
	return nil
}

// followUp calls [Hooks.FollowUp] if set. Returns messages to inject,
// or nil to stop the loop.
func (h *Hooks) followUp(ctx context.Context, msgs []Message) []Message {
	if h.FollowUp != nil {
		return h.FollowUp(ctx, msgs)
	}
	return nil
}

// Middleware wraps tool execution. It receives the [ai.ToolCall] and
// a [ToolRunner] that executes the tool. The middleware controls whether
// the tool runs by calling or skipping next.
//
// Middleware must be safe for concurrent use when tools run in parallel.
type Middleware func(ctx context.Context, call ai.ToolCall, next ToolRunner) (ai.ToolResult, error)

// ToolRunner executes a tool call. Call it to proceed; skip it to block.
type ToolRunner func(ctx context.Context) (ai.ToolResult, error)

// Chain composes middleware left-to-right. The first middleware is the
// outermost wrapper.
func Chain(mw ...Middleware) Middleware {
	switch len(mw) {
	case 0:
		return nil
	case 1:
		return mw[0]
	}
	return func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		runner := next
		for i := len(mw) - 1; i >= 0; i-- {
			current := mw[i]
			inner := runner
			runner = func(ctx context.Context) (ai.ToolResult, error) {
				return current(ctx, call, inner)
			}
		}
		return runner(ctx)
	}
}
