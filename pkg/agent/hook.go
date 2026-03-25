package agent

import (
	"context"

	"github.com/sonnes/pi-go/pkg/ai"
)

// HookEvent identifies when a hook fires in the agent lifecycle.
type HookEvent string

const (
	// HookBeforeCall fires before each LLM call. Hooks can filter
	// agent messages or override the [ai.Message] slice sent to the model.
	HookBeforeCall HookEvent = "before_call"

	// HookBeforeTool fires before a tool executes. Hooks can deny
	// execution or override the tool call.
	HookBeforeTool HookEvent = "before_tool"

	// HookAfterTool fires after a tool executes. Hooks can override
	// the tool result.
	HookAfterTool HookEvent = "after_tool"

	// HookAfterTurn fires after each turn completes. Hooks can replace
	// the message history (e.g. for compaction or steering).
	HookAfterTurn HookEvent = "after_turn"

	// HookBeforeStop fires when the agent would stop (no tool calls).
	// Hooks can inject follow-up messages to continue the loop.
	HookBeforeStop HookEvent = "before_stop"
)

// Hook is a lifecycle callback. The fields populated in [HookInput] and
// the fields read from [HookOutput] depend on the event.
type Hook func(ctx context.Context, input *HookInput) (*HookOutput, error)

// HookInput carries event-specific data to a [Hook].
type HookInput struct {
	// Event identifies which lifecycle point fired this hook.
	Event HookEvent

	// Messages is the current conversation history. Always present.
	Messages []Message

	// Turn is set for [HookAfterTurn] only.
	Turn *TurnResult

	// ToolCall is set for [HookBeforeTool] and [HookAfterTool].
	ToolCall *ai.ToolCall

	// ToolResult is set for [HookAfterTool] only.
	ToolResult *ai.ToolResult
}

// HookOutput controls agent behavior. Which fields are read depends
// on the event — see each field's doc comment.
type HookOutput struct {
	// Messages filters or transforms agent messages.
	//
	// [HookBeforeCall]: replaces the agent messages used for LLM conversion.
	// Subsequent hooks in the chain see this filtered list.
	//
	// [HookAfterTurn]: replaces the agent's message history.
	// nil means no change.
	Messages []Message

	// LLMMessages overrides the final [ai.Message] slice sent to the model.
	// Only read for [HookBeforeCall]. Takes precedence over Messages —
	// when set, Messages filtering still applies to subsequent hooks
	// but the LLM sees this slice.
	LLMMessages []ai.Message

	// Deny blocks tool execution. Only read for [HookBeforeTool].
	Deny bool

	// DenyReason explains why execution was denied.
	// Only read for [HookBeforeTool] when Deny is true.
	DenyReason string

	// ToolCall overrides the tool call arguments.
	// Only read for [HookBeforeTool].
	ToolCall *ai.ToolCall

	// ToolResult overrides the tool result.
	// Only read for [HookAfterTool].
	ToolResult *ai.ToolResult

	// FollowUp injects messages to continue the loop.
	// Only read for [HookBeforeStop]. A non-empty slice
	// prevents the agent from stopping.
	FollowUp []Message
}

// TurnResult is the public view of a completed turn, passed to hooks.
type TurnResult struct {
	AssistantMsg ai.Message
	ToolResults  []ai.Message
	Usage        ai.Usage
}

// hooks is the internal hook registry, keyed by event.
type hooks map[HookEvent][]Hook

// runBeforeCall executes [HookBeforeCall] hooks and returns the
// [ai.Message] slice for the LLM. Falls back to [LLMMessages] when
// no hook sets LLMMessages.
func (h hooks) runBeforeCall(
	ctx context.Context,
	msgs []Message,
) ([]ai.Message, error) {
	handlers := h[HookBeforeCall]
	if len(handlers) == 0 {
		return LLMMessages(msgs), nil
	}

	current := msgs
	var llmMsgs []ai.Message

	for _, hook := range handlers {
		out, err := hook(ctx, &HookInput{
			Event:    HookBeforeCall,
			Messages: current,
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			continue
		}
		if out.Messages != nil {
			current = out.Messages
		}
		if out.LLMMessages != nil {
			llmMsgs = out.LLMMessages
		}
	}

	if llmMsgs != nil {
		return llmMsgs, nil
	}
	return LLMMessages(current), nil
}

// runBeforeTool executes [HookBeforeTool] hooks. Returns a non-nil
// [HookOutput] with Deny=true if any hook blocks execution.
func (h hooks) runBeforeTool(
	ctx context.Context,
	msgs []Message,
	tc ai.ToolCall,
) (*HookOutput, error) {
	for _, hook := range h[HookBeforeTool] {
		out, err := hook(ctx, &HookInput{
			Event:    HookBeforeTool,
			Messages: msgs,
			ToolCall: &tc,
		})
		if err != nil {
			return &HookOutput{Deny: true, DenyReason: err.Error()}, nil
		}
		if out != nil && out.Deny {
			return out, nil
		}
	}
	return nil, nil
}

// runAfterTool executes [HookAfterTool] hooks. Each hook sees the
// previous hook's modified result. Returns the final tool result.
func (h hooks) runAfterTool(
	ctx context.Context,
	msgs []Message,
	tc ai.ToolCall,
	result ai.ToolResult,
) (ai.ToolResult, error) {
	for _, hook := range h[HookAfterTool] {
		out, err := hook(ctx, &HookInput{
			Event:      HookAfterTool,
			Messages:   msgs,
			ToolCall:   &tc,
			ToolResult: &result,
		})
		if err != nil {
			return result, err
		}
		if out != nil && out.ToolResult != nil {
			result = *out.ToolResult
		}
	}
	return result, nil
}

// runAfterTurn executes [HookAfterTurn] hooks. Returns replacement
// messages, or nil if the history should not change.
func (h hooks) runAfterTurn(
	ctx context.Context,
	msgs []Message,
	tr TurnResult,
) ([]Message, error) {
	var replaced []Message
	for _, hook := range h[HookAfterTurn] {
		out, err := hook(ctx, &HookInput{
			Event:    HookAfterTurn,
			Messages: msgs,
			Turn:     &tr,
		})
		if err != nil {
			return nil, err
		}
		if out != nil && out.Messages != nil {
			replaced = out.Messages
			msgs = replaced
		}
	}
	return replaced, nil
}

// runBeforeStop executes [HookBeforeStop] hooks. Returns follow-up
// messages to continue the loop, or nil to stop.
func (h hooks) runBeforeStop(
	ctx context.Context,
	msgs []Message,
) ([]Message, error) {
	for _, hook := range h[HookBeforeStop] {
		out, err := hook(ctx, &HookInput{
			Event:    HookBeforeStop,
			Messages: msgs,
		})
		if err != nil {
			return nil, err
		}
		if out != nil && len(out.FollowUp) > 0 {
			return out.FollowUp, nil
		}
	}
	return nil, nil
}
