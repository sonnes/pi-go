package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Default is the standard [Agent] implementation that manages an
// agentic conversation loop.
//
// Loop flow:
//
//	agent_start
//	  message_start/end (user messages)
//	  turn_start
//	    buildPrompt → ai.StreamText → streamTurn
//	    message_start/update/end (assistant)
//	    [if tool_use: executeTools → message_start/end (tool results)]
//	  turn_end
//	  ... repeat if tool_use and under maxTurns ...
//	agent_end
type Default struct {
	config   config
	toolMap  map[string]ai.Tool
	toolInfo []ai.ToolInfo

	mu       sync.Mutex
	running  bool
	messages []Message
	err      error
}

var _ Agent = (*Default)(nil)

// New creates a new [Default] agent with the given model and options.
func New(model ai.Model, opts ...Option) *Default {
	c := config{model: model}
	for _, opt := range opts {
		opt(&c)
	}

	toolMap := make(map[string]ai.Tool, len(c.tools))
	toolInfo := make([]ai.ToolInfo, len(c.tools))
	for i, t := range c.tools {
		info := t.Info()
		toolMap[info.Name] = t
		toolInfo[i] = info
	}

	msgs := make([]Message, len(c.history))
	copy(msgs, c.history)

	return &Default{
		config:   c,
		toolMap:  toolMap,
		toolInfo: toolInfo,
		messages: msgs,
	}
}

// Send adds a user message and runs the agent loop.
func (a *Default) Send(ctx context.Context, input string) *EventStream {
	return a.SendMessages(ctx, NewLLMMessage(ai.UserMessage(input)))
}

// SendMessages adds messages and runs the agent loop.
func (a *Default) SendMessages(ctx context.Context, msgs ...Message) *EventStream {
	return a.run(ctx, msgs)
}

// Continue resumes from current message state without adding new messages.
func (a *Default) Continue(ctx context.Context) *EventStream {
	return a.run(ctx, nil)
}

// Messages returns a copy of the current conversation history.
func (a *Default) Messages() []Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.messages) == 0 {
		return nil
	}
	out := make([]Message, len(a.messages))
	copy(out, a.messages)
	return out
}

// IsRunning reports whether the agent loop is currently executing.
func (a *Default) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// Err returns the last error encountered during the agent loop, or nil.
func (a *Default) Err() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// run starts the agent loop. It acquires the mutex to check for concurrent
// runs and set up initial state, then launches the loop in a goroutine
// via [NewStream].
func (a *Default) run(ctx context.Context, newMsgs []Message) *EventStream {
	a.mu.Lock()

	if a.running {
		a.mu.Unlock()
		return ErrStream(errors.New("agent: already streaming"))
	}

	a.running = true
	a.err = nil
	a.messages = append(a.messages, newMsgs...)
	inputMsgs := newMsgs
	a.mu.Unlock()

	return NewStream(func(push func(Event)) {
		defer a.stop()
		a.loop(ctx, push, inputMsgs)
	})
}

// stop marks the agent as no longer running.
func (a *Default) stop() {
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
}

// turnResult holds the output of a single turn, returned by executeTurn
// so the caller owns all mutation of loop-level state.
type turnResult struct {
	assistantMsg ai.Message
	toolResults  []ai.Message
	usage        ai.Usage
	cont         bool // true = tool calls made, keep looping
}

// loop is the core agent loop, running inside the [NewStream] producer goroutine.
func (a *Default) loop(
	ctx context.Context,
	push func(Event),
	inputMsgs []Message,
) {
	var (
		totalUsage  ai.Usage
		newMessages []ai.Message
		loopErr     error
	)

	push(Event{Type: EventAgentStart})

	defer func() {
		a.mu.Lock()
		a.err = loopErr
		a.mu.Unlock()

		push(Event{
			Type:     EventAgentEnd,
			Messages: newMessages,
			Usage:    totalUsage,
			Err:      loopErr,
		})
	}()

	system, err := a.renderSystem(ctx)
	if err != nil {
		loopErr = err
		return
	}

	emitMessages(push, inputMsgs)

	for turn := 0; ; turn++ {
		if a.config.maxTurns > 0 && turn >= a.config.maxTurns {
			return
		}
		if ctx.Err() != nil {
			loopErr = ctx.Err()
			return
		}

		tr, err := a.executeTurn(ctx, push, system)
		if err != nil {
			loopErr = err
			return
		}

		totalUsage = addUsage(totalUsage, tr.usage)

		a.messages = append(a.messages, NewLLMMessage(tr.assistantMsg))
		newMessages = append(newMessages, tr.assistantMsg)

		for _, trMsg := range tr.toolResults {
			a.messages = append(a.messages, NewLLMMessage(trMsg))
			newMessages = append(newMessages, trMsg)
		}

		if !tr.cont {
			return
		}
	}
}

// executeTurn runs a single turn of the agent loop. It emits TurnStart
// at entry and defers TurnEnd so the pair is always balanced — even on
// errors or early returns. The returned [turnResult] carries all outputs;
// the caller is responsible for updating loop-level state.
func (a *Default) executeTurn(
	ctx context.Context,
	push func(Event),
	system string,
) (tr turnResult, err error) {
	var turnMsg *ai.Message

	push(Event{Type: EventTurnStart})
	defer func() {
		push(Event{
			Type:        EventTurnEnd,
			Message:     turnMsg,
			ToolResults: tr.toolResults,
		})
	}()

	prompt := a.buildPrompt(system)
	aiStream := ai.StreamText(ctx, a.config.model, prompt, a.config.streamOpts...)

	assistantMsg, err := streamTurn(push, aiStream)
	if err != nil {
		return tr, err
	}

	tr.assistantMsg = *assistantMsg
	tr.usage = assistantMsg.Usage
	turnMsg = assistantMsg

	if assistantMsg.StopReason != ai.StopReasonToolUse {
		return tr, nil
	}

	toolCalls := extractToolCalls(assistantMsg)
	tr.toolResults = a.executeTools(ctx, push, toolCalls)
	tr.cont = true

	emitMessages(push, wrapMessages(tr.toolResults))

	return tr, nil
}

// renderSystem renders the system prompt sections once. Panics in
// [PromptSection.Content] are recovered and returned as errors.
func (a *Default) renderSystem(ctx context.Context) (system string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("agent: panic in system prompt section: %v", r)
		}
	}()
	return renderSystemPrompt(ctx, a.config.systemPrompt), nil
}

// buildPrompt assembles an [ai.Prompt] from a pre-rendered system string
// and the current message history.
func (a *Default) buildPrompt(system string) ai.Prompt {
	return ai.Prompt{
		System:   system,
		Messages: LLMMessages(a.messages),
		Tools:    a.toolInfo,
	}
}

// renderSystemPrompt concatenates all prompt sections with double newlines.
func renderSystemPrompt(ctx context.Context, p Prompt) string {
	if len(p.Sections) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, section := range p.Sections {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(section.Content(ctx))
	}
	return sb.String()
}

// streamTurn consumes an [ai.EventStream] from the provider, emitting
// agent-level message events. Returns the final assistant message or an error.
func streamTurn(
	push func(Event),
	aiStream *ai.EventStream,
) (*ai.Message, error) {
	var (
		started  bool
		finalMsg *ai.Message
	)

	for evt, err := range aiStream.Events() {
		if err != nil {
			return nil, err
		}

		if !started && evt.Message != nil {
			push(Event{
				Type:    EventMessageStart,
				Message: evt.Message,
			})
			started = true
		}

		switch evt.Type {
		case ai.EventDone:
			finalMsg = evt.Message
		default:
			if started {
				push(Event{
					Type:           EventMessageUpdate,
					Message:        evt.Message,
					AssistantEvent: &evt,
				})
			}
		}
	}

	if started {
		push(Event{
			Type:    EventMessageEnd,
			Message: finalMsg,
		})
	}

	if finalMsg == nil {
		return nil, errors.New("agent: provider returned no message")
	}

	return finalMsg, nil
}

// extractToolCalls filters [ai.ToolCall] content blocks from a message.
func extractToolCalls(msg *ai.Message) []ai.ToolCall {
	var calls []ai.ToolCall
	for _, c := range msg.Content {
		if tc, ok := ai.AsContent[ai.ToolCall](c); ok {
			calls = append(calls, tc)
		}
	}
	return calls
}

// executeTools runs tool calls, emitting execution events for each.
// If all tools in the batch are marked parallel-safe, they run concurrently.
// Otherwise, they run sequentially. Per-tool panics are recovered and
// converted to error results.
func (a *Default) executeTools(
	ctx context.Context,
	push func(Event),
	calls []ai.ToolCall,
) []ai.Message {
	allParallel := len(calls) > 1
	for _, tc := range calls {
		if t, ok := a.toolMap[tc.Name]; ok {
			if !t.Info().Parallel {
				allParallel = false
				break
			}
		} else {
			allParallel = false
			break
		}
	}

	results := make([]ai.Message, len(calls))

	if allParallel {
		var wg sync.WaitGroup
		for i, tc := range calls {
			wg.Add(1)
			go func(i int, tc ai.ToolCall) {
				defer wg.Done()
				results[i] = a.executeSingleTool(ctx, push, tc)
			}(i, tc)
		}
		wg.Wait()
	} else {
		for i, tc := range calls {
			results[i] = a.executeSingleTool(ctx, push, tc)
		}
	}

	return results
}

// executeSingleTool runs one tool call with panic recovery, emitting
// start/update/end events.
func (a *Default) executeSingleTool(
	ctx context.Context,
	push func(Event),
	tc ai.ToolCall,
) (result ai.Message) {
	push(Event{
		Type:       EventToolExecutionStart,
		ToolCallID: tc.ID,
		ToolName:   tc.Name,
		Args:       tc.Arguments,
	})

	defer func() {
		if r := recover(); r != nil {
			result = finishToolError(push, tc, fmt.Sprintf("panic: %v", r))
		}
	}()

	tool, ok := a.toolMap[tc.Name]
	if !ok {
		return finishToolError(push, tc, fmt.Sprintf("tool %q not found", tc.Name))
	}

	inputJSON, err := json.Marshal(tc.Arguments)
	if err != nil {
		return finishToolError(push, tc, fmt.Sprintf("failed to marshal arguments: %v", err))
	}

	req := ai.ToolCallReq{
		ID:    tc.ID,
		Name:  tc.Name,
		Input: string(inputJSON),
		OnUpdate: func(partial ai.ToolResult) {
			push(Event{
				Type:          EventToolExecutionUpdate,
				ToolCallID:    tc.ID,
				ToolName:      tc.Name,
				PartialResult: partial.Content,
			})
		},
	}

	toolResult, err := tool.Run(ctx, req)
	if err != nil {
		return finishToolError(push, tc, err.Error())
	}

	if toolResult.IsError {
		return finishToolError(push, tc, toolResult.Content)
	}

	msg := ai.ToolResultMessage(tc.ID, tc.Name, ai.Text{Text: toolResult.Content})
	push(Event{
		Type:       EventToolExecutionEnd,
		ToolCallID: tc.ID,
		ToolName:   tc.Name,
		Result:     toolResult.Content,
	})
	return msg
}

// finishToolError creates an error tool result message and emits the
// [EventToolExecutionEnd] event.
func finishToolError(push func(Event), tc ai.ToolCall, errMsg string) ai.Message {
	msg := ai.ErrorToolResultMessage(tc.ID, tc.Name, errMsg)
	push(Event{
		Type:       EventToolExecutionEnd,
		ToolCallID: tc.ID,
		ToolName:   tc.Name,
		Result:     errMsg,
		IsError:    true,
	})
	return msg
}

// emitMessages pushes message_start/message_end events for each [LLMMessage].
func emitMessages(push func(Event), msgs []Message) {
	for _, m := range msgs {
		lm, ok := AsLLMMessage(m)
		if !ok {
			continue
		}
		push(Event{
			Type:    EventMessageStart,
			Message: &lm.Message,
		})
		push(Event{
			Type:    EventMessageEnd,
			Message: &lm.Message,
		})
	}
}

// wrapMessages wraps a slice of [ai.Message] into [Message] values.
func wrapMessages(msgs []ai.Message) []Message {
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = NewLLMMessage(m)
	}
	return out
}

// addUsage sums two [ai.Usage] values.
func addUsage(a, b ai.Usage) ai.Usage {
	return ai.Usage{
		Input:      a.Input + b.Input,
		Output:     a.Output + b.Output,
		CacheRead:  a.CacheRead + b.CacheRead,
		CacheWrite: a.CacheWrite + b.CacheWrite,
		Total:      a.Total + b.Total,
		Cost: ai.UsageCost{
			Input:      a.Cost.Input + b.Cost.Input,
			Output:     a.Cost.Output + b.Cost.Output,
			CacheRead:  a.Cost.CacheRead + b.Cost.CacheRead,
			CacheWrite: a.Cost.CacheWrite + b.Cost.CacheWrite,
			Total:      a.Cost.Total + b.Cost.Total,
		},
	}
}
