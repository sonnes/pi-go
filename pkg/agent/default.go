package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Default is the standard [Agent] implementation that manages an
// agentic conversation loop.
//
// Loop flow, per [Default.Run]:
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
// Caller-supplied input messages are not echoed as message_start/end
// events — only messages produced by the loop are emitted.
type Default struct {
	config   config
	toolMap  map[string]ai.Tool
	toolInfo []ai.ToolInfo

	mu       sync.Mutex
	running  bool
	messages []ai.Message
}

var _ Agent = (*Default)(nil)

// New creates a new [Default] agent for lm, configured via options.
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
		// Server tools are advertised to the model but executed by the
		// provider, so they're omitted from toolMap to keep the
		// executor focused on function tools only.
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

// Run implements [Agent]. It appends msgs to the history and executes
// the loop, streaming events on the returned [Stream].
func (a *Default) Run(ctx context.Context, msgs ...ai.Message) *Stream {
	if a.config.lm == nil {
		return errStream(errors.New("agent: no model configured; pass a LanguageModel to New"))
	}
	return NewStream(func(push func(Event)) ([]ai.Message, error) {
		return a.run(ctx, msgs, push)
	})
}

// run guards against concurrent runs, appends the input messages, and
// executes the loop. It is the producer behind [Default.Run]'s stream.
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
// resources, so it is a no-op.
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

// replaceHistory swaps the conversation history (AfterTurn hooks).
func (a *Default) replaceHistory(msgs []ai.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = msgs
}

// turnResult holds the output of a single turn, returned by executeTurn
// so the caller owns all mutation of loop-level state.
type turnResult struct {
	assistantMsg ai.Message
	toolResults  []ai.Message
	usage        ai.Usage
	cont         bool // true = tool calls made, keep looping
}

// loop is the core agent loop, running inside the stream's producer
// goroutine. On success it ends with [EventAgentEnd]; on failure it
// returns the messages produced so far along with the error, and no
// agent_end is emitted.
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

		totalUsage = addUsage(totalUsage, tr.usage)

		a.appendHistory(tr.assistantMsg)
		newMessages = append(newMessages, tr.assistantMsg)

		for _, trMsg := range tr.toolResults {
			a.appendHistory(trMsg)
			newMessages = append(newMessages, trMsg)
		}

		// AfterTurn: let hooks replace the message history.
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

		// FollowUp: let hooks inject messages to keep the loop going.
		// Check that another turn is allowed before injecting.
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

// executeTurn runs a single turn of the agent loop. It emits TurnStart
// at entry and defers TurnEnd so the pair is always balanced — even on
// errors or early returns. The returned [turnResult] carries all outputs;
// the caller is responsible for updating loop-level state.
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

	// Server-tool calls are executed by the provider; the agent only runs
	// client-side function tools.
	toolCalls := filterFunctionCalls(assistantMsg.ToolCalls())
	if len(toolCalls) == 0 {
		return tr, nil
	}

	tr.toolResults = a.executeTools(ctx, push, toolCalls)
	tr.cont = true

	return tr, nil
}

// filterFunctionCalls returns the subset of tool calls that the agent should
// execute locally — i.e. function tools, not provider-executed server tools.
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

// streamTurn consumes an [ai.EventStream] from the provider, emitting
// agent-level message events with incremental partial message snapshots.
//
// The partial message is accumulated from provider deltas so that every
// [EventMessageUpdate] carries a non-nil Message snapshot. This matches
// the pi-mono (TypeScript) behavior and lets consumers choose between
// reading the delta ([Event.AssistantEvent]) or the snapshot ([Event.Message]).
//
// [EventMessageStart] fires on the first event (before any content
// arrives). [EventMessageEnd] carries the provider's final
// authoritative message, returned by the stream's Wait.
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
			// Keep message_start/message_end paired even when the stream
			// errors mid-flight: emit message_end with the partial we have
			// accumulated so consumers tracking message scope don't see a
			// dangling start.
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

// accumulateEvent updates partial with the delta from evt, building up
// the message's Content slice incrementally as provider events arrive.
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

// ensureContentIndex grows partial.Content so that index i exists,
// using zero as fill for any gaps.
func ensureContentIndex(m *ai.Message, i int, zero ai.Content) {
	for len(m.Content) <= i {
		m.Content = append(m.Content, zero)
	}
}

// snapshotMessage returns a shallow copy of m with a copied Content slice,
// so that later accumulation does not mutate previously published snapshots.
func snapshotMessage(m *ai.Message) *ai.Message {
	cp := *m
	cp.Content = make([]ai.Content, len(m.Content))
	copy(cp.Content, m.Content)
	return &cp
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
// start/update/end events. BeforeTool hooks can deny execution;
// AfterTool hooks can modify the result.
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

	// BeforeTool: hooks can deny execution.
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

	// AfterTool: hooks can modify the result.
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

// runTool executes a single tool call and returns the [ai.ToolResult].
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

// finishToolError creates an error tool result message and emits the
// [EventToolExecutionEnd] event followed by message_start/message_end
// for the tool result.
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

// emitToolResult publishes the message_start / message_end pair for a
// tool result message, immediately after [EventToolExecutionEnd]. The
// pair is emitted per-tool so the lifecycle order matches the spec
// diagram in docs/concepts/agent/streaming.md.
func emitToolResult(push func(Event), msg ai.Message) {
	push(Event{Type: EventMessageStart, Message: &msg})
	push(Event{Type: EventMessageEnd, Message: &msg})
}

// emitMessages pushes message_start/message_end events for each
// message. input marks the events as Input=true on the published
// [Event], so consumers persisting from the event stream can skip
// caller-supplied messages they have already stored.
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
