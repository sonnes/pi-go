package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Default is the standard [Agent] implementation. It manages an agentic
// conversation loop.
//
// Loop flow of [Default.Run]:
//
//	agent_start
//	  turn_start
//	    buildPrompt → streamText → streamTurn
//	    message_start/update/end (assistant)
//	    [if tool_use, for each call: tool_execution_start/end →
//	     message_start/end (tool result)]
//	  turn_end
//	  ... repeat if tool_use and under maxTurns ...
//	agent_end   ← success only; failures end the stream with an error
//
// The loop does not echo the input messages of the caller as
// message_start and message_end events. It emits these events only for
// the messages that it produces.
type Default struct {
	config   config
	toolMap  map[string]ai.Tool
	toolInfo []ai.ToolInfo

	mu       sync.Mutex
	running  bool
	messages []ai.Message
}

var _ Agent = (*Default)(nil)

// New creates a new [Default] agent for lm. The opts values configure it.
func New(lm ai.LanguageModel, opts ...Option) *Default {
	c := config{lm: lm}
	for _, opt := range opts {
		opt(&c)
	}

	toolMap := make(map[string]ai.Tool, len(c.tools))
	toolInfo := make([]ai.ToolInfo, len(c.tools))
	for i, t := range c.tools {
		info := t.Info()
		toolInfo[i] = info
		// The agent advertises server tools to the model, but the
		// provider runs them. Therefore toolMap does not hold them,
		// and the executor works on function tools only.
		if info.Kind == ai.ToolKindServer {
			continue
		}
		toolMap[info.Name] = t
	}

	msgs := make([]ai.Message, len(c.history))
	copy(msgs, c.history)

	return &Default{
		config:   c,
		toolMap:  toolMap,
		toolInfo: toolInfo,
		messages: msgs,
	}
}

// Run implements [Agent]. It appends msgs to the history and runs the
// loop. It streams the events on the returned [Stream].
func (a *Default) Run(ctx context.Context, msgs ...ai.Message) *Stream {
	if a.config.lm == nil {
		return ErrStream(errors.New("agent: no model configured; pass a LanguageModel to New"))
	}
	return NewStream(func(push func(Event)) ([]ai.Message, error) {
		return a.run(ctx, msgs, push)
	})
}

// run guards against concurrent runs, appends the input messages, and
// runs the loop. It is the producer behind the stream of [Default.Run].
func (a *Default) run(
	ctx context.Context,
	newMsgs []ai.Message,
	push func(Event),
) ([]ai.Message, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil, errors.New("agent: already running")
	}
	a.running = true
	a.messages = append(a.messages, newMsgs...)
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	return a.loop(ctx, push)
}

// Close implements [Agent]. The in-process loop holds no backend
// resources, so Close does nothing.
func (a *Default) Close() error { return nil }

// Messages returns a copy of the current conversation history.
func (a *Default) Messages() []ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.messages) == 0 {
		return nil
	}
	out := make([]ai.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

// history returns the current conversation history without copying.
// Callers must treat the result as read-only.
func (a *Default) history() []ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.messages
}

// appendHistory appends msgs to the conversation history.
func (a *Default) appendHistory(msgs ...ai.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, msgs...)
}

// replaceHistory replaces the conversation history. The AfterTurn hooks
// use it.
func (a *Default) replaceHistory(msgs []ai.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = msgs
}

// turnResult holds the output of a single turn. executeTurn returns it,
// so that the caller owns all changes to loop-level state.
type turnResult struct {
	assistantMsg ai.Message
	toolResults  []ai.Message
	usage        ai.Usage
	cont         bool // true when the turn made tool calls, keep looping
}

// loop is the core agent loop. It runs inside the producer goroutine of
// the stream. On success it ends with [EventAgentEnd]. On an error it
// returns the messages that it produced so far together with the error,
// and it emits no agent_end.
func (a *Default) loop(
	ctx context.Context,
	push func(Event),
) ([]ai.Message, error) {
	var (
		totalUsage  ai.Usage
		newMessages []ai.Message
	)

	push(Event{Type: EventAgentStart})

	for turn := 0; ; turn++ {
		if a.config.maxTurns > 0 && turn >= a.config.maxTurns {
			break
		}
		if ctx.Err() != nil {
			return newMessages, ctx.Err()
		}

		tr, err := a.executeTurn(ctx, push)
		if err != nil {
			return newMessages, err
		}

		totalUsage = totalUsage.Add(tr.usage)

		a.appendHistory(tr.assistantMsg)
		newMessages = append(newMessages, tr.assistantMsg)

		for _, trMsg := range tr.toolResults {
			a.appendHistory(trMsg)
			newMessages = append(newMessages, trMsg)
		}

		// AfterTurn: the hooks can replace the message history.
		hookTR := TurnResult{
			AssistantMsg: tr.assistantMsg,
			ToolResults:  tr.toolResults,
			Usage:        tr.usage,
		}
		replaced, err := a.config.hooks.runAfterTurn(ctx, a.history(), hookTR)
		if err != nil {
			return newMessages, err
		}
		if replaced != nil {
			a.replaceHistory(replaced)
		}

		if tr.cont {
			continue
		}

		// FollowUp: the hooks can inject messages to continue the loop.
		// First make sure that another turn is allowed.
		nextTurn := turn + 1
		if a.config.maxTurns > 0 && nextTurn >= a.config.maxTurns {
			break
		}
		followMsgs, err := a.config.hooks.runBeforeStop(ctx, a.history())
		if err != nil {
			return newMessages, err
		}
		if len(followMsgs) == 0 {
			break
		}
		a.appendHistory(followMsgs...)
		newMessages = append(newMessages, followMsgs...)
		emitMessages(push, followMsgs, true)
	}

	push(Event{
		Type:     EventAgentEnd,
		Messages: newMessages,
		Usage:    totalUsage,
	})

	return newMessages, nil
}

// executeTurn runs a single turn of the agent loop. It emits TurnStart at
// entry and defers TurnEnd, so the pair is always balanced, even on an
// error or an early return. The returned [turnResult] carries all
// outputs. The caller updates the loop-level state.
func (a *Default) executeTurn(
	ctx context.Context,
	push func(Event),
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

	prompt, err := a.buildPrompt(ctx)
	if err != nil {
		return tr, err
	}
	aiStream := a.streamText(ctx, prompt)

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

	// The provider runs the server-tool calls. The agent runs only the
	// client-side function tools.
	toolCalls := filterFunctionCalls(assistantMsg.ToolCalls())
	if len(toolCalls) == 0 {
		return tr, nil
	}

	tr.toolResults = a.executeTools(ctx, push, toolCalls)
	tr.cont = true

	return tr, nil
}

// filterFunctionCalls returns the tool calls that the agent runs locally,
// that is, the function tools. It removes the server tools, because the
// provider runs them.
func filterFunctionCalls(calls []ai.ToolCall) []ai.ToolCall {
	out := calls[:0:0]
	for _, tc := range calls {
		if tc.Server {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// streamText streams a turn from the bound [ai.LanguageModel].
func (a *Default) streamText(ctx context.Context, p ai.Prompt) *ai.EventStream {
	return a.config.lm.StreamText(ctx, p, a.config.streamOpts...)
}

// buildPrompt assembles an [ai.Prompt] from the system prompt and the
// current message history.
func (a *Default) buildPrompt(ctx context.Context) (ai.Prompt, error) {
	llmMsgs, err := a.config.hooks.runBeforeCall(ctx, a.history())
	if err != nil {
		return ai.Prompt{}, err
	}
	return ai.Prompt{
		System:   a.config.systemPrompt,
		Messages: llmMsgs,
		Tools:    a.toolInfo,
	}, nil
}

// streamTurn reads an [ai.EventStream] from the provider. It emits
// agent-level message events with incremental snapshots of the partial
// message.
//
// streamTurn accumulates the partial message from the provider deltas, so
// every [EventMessageUpdate] carries a non-nil Message snapshot. This
// behavior matches pi-mono (TypeScript). A consumer reads either the
// delta ([Event.AssistantEvent]) or the snapshot ([Event.Message]).
//
// [EventMessageStart] fires on the first event, before any content
// arrives. [EventMessageEnd] carries the final authoritative message of
// the provider, which the Wait method of the stream returns.
func streamTurn(
	push func(Event),
	aiStream *ai.EventStream,
) (*ai.Message, error) {
	var (
		started bool
		partial = &ai.Message{Role: ai.RoleAssistant}
	)

	for evt, err := range aiStream.Events() {
		if err != nil {
			// Keep message_start and message_end paired, even when the
			// stream fails in mid-flight. Emit message_end with the
			// accumulated partial message. Then a consumer that tracks
			// the message scope does not see a dangling start.
			if started {
				push(Event{
					Type:    EventMessageEnd,
					Message: snapshotMessage(partial),
				})
			}
			return nil, err
		}

		accumulateEvent(partial, &evt)

		if !started {
			push(Event{
				Type:    EventMessageStart,
				Message: snapshotMessage(partial),
			})
			started = true
		}
		push(Event{
			Type:           EventMessageUpdate,
			Message:        snapshotMessage(partial),
			AssistantEvent: &evt,
		})
	}

	finalMsg, err := aiStream.Wait()
	if err != nil {
		return nil, err
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

// accumulateEvent updates partial with the delta from evt. It builds the
// Content slice of the message step by step as the provider events
// arrive.
func accumulateEvent(partial *ai.Message, evt *ai.Event) {
	switch evt.Type {
	case ai.EventTextStart:
		ensureContentIndex(partial, evt.ContentIndex, ai.Text{})
	case ai.EventTextDelta:
		ensureContentIndex(partial, evt.ContentIndex, ai.Text{})
		if t, ok := partial.Content[evt.ContentIndex].(ai.Text); ok {
			t.Text += evt.Delta
			partial.Content[evt.ContentIndex] = t
		}
	case ai.EventTextEnd:
		ensureContentIndex(partial, evt.ContentIndex, ai.Text{})
		if t, ok := partial.Content[evt.ContentIndex].(ai.Text); ok {
			t.Text = evt.Content
			partial.Content[evt.ContentIndex] = t
		}
	case ai.EventThinkStart:
		ensureContentIndex(partial, evt.ContentIndex, ai.Thinking{})
	case ai.EventThinkDelta:
		ensureContentIndex(partial, evt.ContentIndex, ai.Thinking{})
		if t, ok := partial.Content[evt.ContentIndex].(ai.Thinking); ok {
			t.Thinking += evt.Delta
			partial.Content[evt.ContentIndex] = t
		}
	case ai.EventThinkEnd:
		ensureContentIndex(partial, evt.ContentIndex, ai.Thinking{})
		if t, ok := partial.Content[evt.ContentIndex].(ai.Thinking); ok {
			t.Thinking = evt.Content
			partial.Content[evt.ContentIndex] = t
		}
	case ai.EventToolStart:
		ensureContentIndex(partial, evt.ContentIndex, ai.ToolCall{})
	case ai.EventToolEnd:
		if evt.ToolCall != nil {
			ensureContentIndex(partial, evt.ContentIndex, ai.ToolCall{})
			partial.Content[evt.ContentIndex] = *evt.ToolCall
		}
	}
}

// ensureContentIndex grows partial.Content until index i exists. It fills
// any gaps with zero.
func ensureContentIndex(m *ai.Message, i int, zero ai.Content) {
	for len(m.Content) <= i {
		m.Content = append(m.Content, zero)
	}
}

// snapshotMessage returns a shallow copy of m with a copy of the Content
// slice. Later accumulation then does not change the snapshots that
// streamTurn published before.
func snapshotMessage(m *ai.Message) *ai.Message {
	cp := *m
	cp.Content = make([]ai.Content, len(m.Content))
	copy(cp.Content, m.Content)
	return &cp
}

// executeTools runs the tool calls and emits execution events for each
// call. If all tools in the batch are parallel-safe, they run
// concurrently. If not, they run in sequence. executeTools recovers a
// panic in one tool and returns an error result for that tool.
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

// executeSingleTool runs one tool call and recovers from a panic. It
// emits the start, update, and end events. A BeforeTool hook can deny the
// run. An AfterTool hook can modify the result.
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

	// BeforeTool: the hooks can deny the run.
	denied, err := a.config.hooks.runBeforeTool(ctx, a.history(), tc)
	if err != nil {
		return finishToolError(push, tc, err.Error())
	}
	if denied != nil && denied.Deny {
		reason := denied.DenyReason
		if reason == "" {
			reason = "tool execution was blocked"
		}
		return finishToolError(push, tc, reason)
	}

	toolResult, err := a.runTool(ctx, push, tc)
	if err != nil {
		return finishToolError(push, tc, err.Error())
	}

	// AfterTool: the hooks can modify the result.
	toolResult, err = a.config.hooks.runAfterTool(ctx, a.history(), tc, toolResult)
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
	emitToolResult(push, msg)
	return msg
}

// runTool runs a single tool call and returns the [ai.ToolResult].
func (a *Default) runTool(
	ctx context.Context,
	push func(Event),
	tc ai.ToolCall,
) (ai.ToolResult, error) {
	tool, ok := a.toolMap[tc.Name]
	if !ok {
		return ai.NewErrorResult(
			tc.ID,
			fmt.Sprintf("tool %q not found", tc.Name),
		), nil
	}

	inputJSON, err := json.Marshal(tc.Arguments)
	if err != nil {
		return ai.NewErrorResult(
			tc.ID,
			fmt.Sprintf("failed to marshal arguments: %v", err),
		), nil
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
		return ai.NewErrorResult(tc.ID, err.Error()), nil
	}

	return toolResult, nil
}

// finishToolError creates an error tool result message. It then emits the
// [EventToolExecutionEnd] event. After that it emits message_start and
// message_end for the tool result.
func finishToolError(push func(Event), tc ai.ToolCall, errMsg string) ai.Message {
	msg := ai.ErrorToolResultMessage(tc.ID, tc.Name, errMsg)
	push(Event{
		Type:       EventToolExecutionEnd,
		ToolCallID: tc.ID,
		ToolName:   tc.Name,
		Result:     errMsg,
		IsError:    true,
	})
	emitToolResult(push, msg)
	return msg
}

// emitToolResult publishes the message_start and message_end pair for a
// tool result message, immediately after [EventToolExecutionEnd]. It
// emits the pair for each tool, so the lifecycle order matches the spec
// diagram in docs/concepts/agent/streaming.md.
func emitToolResult(push func(Event), msg ai.Message) {
	push(Event{Type: EventMessageStart, Message: &msg})
	push(Event{Type: EventMessageEnd, Message: &msg})
}

// emitMessages pushes a message_start and a message_end event for each
// message. The input argument sets Input=true on the published [Event].
// A consumer that stores messages from the event stream then skips the
// caller-supplied messages that it already stored.
func emitMessages(push func(Event), msgs []ai.Message, input bool) {
	for i := range msgs {
		push(Event{
			Type:    EventMessageStart,
			Message: &msgs[i],
			Input:   input,
		})
		push(Event{
			Type:    EventMessageEnd,
			Message: &msgs[i],
			Input:   input,
		})
	}
}
