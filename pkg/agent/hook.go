package agent

import (
	"context"

	"github.com/sonnes/pi-go/pkg/ai"
)

// HookEvent identifies when a hook fires in the agent lifecycle.
type HookEvent string

const (
	// HookBeforeCall fires before each LLM call. A hook can filter or
	// replace the [ai.Message] slice that goes to the model.
	HookBeforeCall HookEvent = "before_call"

	// HookBeforeTool fires before a tool runs. A hook can deny the run
	// or override the tool call.
	HookBeforeTool HookEvent = "before_tool"

	// HookAfterTool fires after a tool runs. A hook can override the
	// tool result.
	HookAfterTool HookEvent = "after_tool"

	// HookAfterTurn fires after each turn is complete. A hook can
	// replace the message history. Compaction and steering are two
	// examples.
	HookAfterTurn HookEvent = "after_turn"

	// HookBeforeStop fires when there are no tool calls and the agent
	// is ready to stop. A hook can inject follow-up messages to
	// continue the loop.
	HookBeforeStop HookEvent = "before_stop"
)

// Hook is a lifecycle callback. The event controls which fields
// [HookInput] carries. The event also controls which fields the agent
// reads from [HookOutput].
type Hook func(ctx context.Context, input *HookInput) (*HookOutput, error)

// HookInput carries event-specific data to a [Hook].
type HookInput struct {
	// Event identifies which lifecycle point fired this hook.
	Event HookEvent

	// Messages is the current conversation history. It is always
	// present.
	Messages []ai.Message

	// Turn has a value for [HookAfterTurn] only.
	Turn *TurnResult

	// ToolCall has a value for [HookBeforeTool] and [HookAfterTool].
	ToolCall *ai.ToolCall

	// ToolResult has a value for [HookAfterTool] only.
	ToolResult *ai.ToolResult
}

// HookOutput controls the behavior of the agent. The event controls which
// fields the agent reads. For details, read the comment on each field.
type HookOutput struct {
	// Messages filters or transforms the conversation messages.
	//
	// [HookBeforeCall]: replaces the messages that go to the model.
	// The next hooks in the chain see this filtered list.
	//
	// [HookAfterTurn]: replaces the message history of the agent.
	// A nil value means no change.
	Messages []ai.Message

	// Deny blocks the tool run. The agent reads it for
	// [HookBeforeTool] only.
	Deny bool

	// DenyReason explains why the agent denied the tool run.
	// The agent reads it for [HookBeforeTool] only, when Deny is true.
	DenyReason string

	// ToolCall overrides the arguments of the tool call.
	// The agent reads it for [HookBeforeTool] only.
	ToolCall *ai.ToolCall

	// ToolResult overrides the tool result.
	// The agent reads it for [HookAfterTool] only.
	ToolResult *ai.ToolResult

	// FollowUp injects messages to continue the loop.
	// The agent reads it for [HookBeforeStop] only. If the slice is
	// not empty, the agent does not stop.
	FollowUp []ai.Message
}

// TurnResult is the public view of a completed turn. The agent passes it
// to the hooks.
type TurnResult struct {
	AssistantMsg ai.Message
	ToolResults  []ai.Message
	Usage        ai.Usage
}

// hooks is the internal hook registry, keyed by event.
type hooks map[HookEvent][]Hook

// runBeforeCall runs the [HookBeforeCall] hooks. It returns the
// [ai.Message] slice for the LLM.
func (h hooks) runBeforeCall(
	ctx context.Context,
	msgs []ai.Message,
) ([]ai.Message, error) {
	current := msgs

	for _, hook := range h[HookBeforeCall] {
		out, err := hook(ctx, &HookInput{
			Event:    HookBeforeCall,
			Messages: current,
		})
		if err != nil {
			return nil, err
		}
		if out != nil && out.Messages != nil {
			current = out.Messages
		}
	}

	return current, nil
}

// runBeforeTool runs the [HookBeforeTool] hooks. If a hook blocks the
// tool, it returns a non-nil [HookOutput] with Deny=true.
func (h hooks) runBeforeTool(
	ctx context.Context,
	msgs []ai.Message,
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

// runAfterTool runs the [HookAfterTool] hooks. Each hook sees the result
// that the previous hook modified. It returns the final tool result.
func (h hooks) runAfterTool(
	ctx context.Context,
	msgs []ai.Message,
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

// runAfterTurn runs the [HookAfterTurn] hooks. It returns replacement
// messages, or nil when the history does not change.
func (h hooks) runAfterTurn(
	ctx context.Context,
	msgs []ai.Message,
	tr TurnResult,
) ([]ai.Message, error) {
	var replaced []ai.Message
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

// runBeforeStop runs the [HookBeforeStop] hooks. It returns follow-up
// messages to continue the loop, or nil to stop.
func (h hooks) runBeforeStop(
	ctx context.Context,
	msgs []ai.Message,
) ([]ai.Message, error) {
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
