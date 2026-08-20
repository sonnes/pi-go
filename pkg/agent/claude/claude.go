package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Agent implements [agent.Agent] with a long-lived Claude Code CLI
// subprocess. The subprocess runs the full agent loop. The package
// starts the subprocess on the first [Agent.Run] with
// `--input-format stream-json`. One subprocess serves many turns. Each
// Run writes one SDKUserMessage to stdin. The Run completes when the CLI
// emits the matching result line.
type Agent struct {
	cfg config

	// model is the caller's [ai.Model]. The package keeps it for the Cost
	// rates. The CLI reports token counts but no cost per category.
	model ai.Model

	// newTransport is the factory for the CLI subprocess. Tests replace it.
	newTransport func(ctx context.Context, cfg config) (transportIface, error)

	mu        sync.Mutex
	running   bool
	sessionID string
	messages  []ai.Message
	// push is the event sink of the active run. It is nil when the agent
	// is idle. The agent ignores events that arrive between runs.
	push func(agent.Event)
	// turnDone receives the end-of-turn signal from the reader for the
	// active run.
	turnDone chan turnResult
	// runCtx is the context of the active run. The agent gives it to the
	// before_tool hooks that answer can_use_tool control requests. It is
	// nil when the agent is idle.
	runCtx context.Context

	// expectAgentStart tells readLoop to publish [agent.EventAgentStart]
	// immediately before its next batch of parser events. runTurn sets
	// this field true before it writes the user line. readLoop clears the
	// field atomically when it publishes the bracket. readLoop does the
	// publish so that agent_start carries the session ID from the
	// subprocess `system/init` line. On a new subprocess, that line
	// arrives after the user line is written.
	expectAgentStart atomic.Bool

	// transport is the active subprocess, created on first Run.
	transport transportIface
	// readerDone closes when the stdout reader goroutine exits.
	readerDone chan struct{}
}

// turnResult carries the accumulated state of one turn from the reader
// to the Run producer goroutine.
type turnResult struct {
	messages  []ai.Message
	usage     ai.Usage
	sessionID string
	err       error
}

var _ agent.Agent = (*Agent)(nil)

// New creates a Claude CLI subprocess [Agent] for model. To create an
// agent from a spec, register [Factory] with the catalog under the
// "claude" kind. The CLI owns its model catalog. The agent therefore
// uses the name or the ID of the model, plus any claude-specific options
// such as [WithCLIPath], [WithAllowedTools], or [WithSessionID].
func New(model ai.Model, opts ...agent.Option) *Agent {
	return newFromConfig(model, agent.ApplyOptions(opts...))
}

// newFromConfig builds an *Agent from a resolved [agent.Config]. It maps
// the agent-level fields onto the claude-local [config]. These fields
// are [agent.Config.Model.Name], [agent.Config.MaxTurns], and
// [agent.Config.History]. The rest of [config] comes from
// [agent.Config.Extensions] under [extensionKey].
func newFromConfig(model ai.Model, ac agent.Config) *Agent {
	cfg := config{cliPath: "claude"}
	if ext, ok := ac.Extensions[extensionKey].(*config); ok && ext != nil {
		cfg = *ext
		if cfg.cliPath == "" {
			cfg.cliPath = "claude"
		}
	}
	if model.Name != "" {
		cfg.model = model.Name
	} else if model.ID != "" {
		cfg.model = model.ID
	}
	if ac.MaxTurns > 0 {
		cfg.maxTurns = ac.MaxTurns
	}
	if len(ac.History) > 0 {
		cfg.history = ac.History
	}
	if ac.SystemPrompt != "" {
		cfg.systemPrompt = ac.SystemPrompt
	}
	if hooks := ac.Hooks[agent.HookBeforeTool]; len(hooks) > 0 {
		cfg.beforeTool = hooks
	}

	var msgs []ai.Message
	if len(cfg.history) > 0 {
		msgs = make([]ai.Message, len(cfg.history))
		copy(msgs, cfg.history)
	}

	return &Agent{
		model:        model,
		cfg:          cfg,
		newTransport: newTransport,
		sessionID:    cfg.sessionID,
		messages:     msgs,
	}
}

// Run implements [agent.Agent]. It appends msgs to the history. It then
// sends the most recent user message, with all its content blocks,
// through the subprocess and runs one turn. The history keeps the
// non-user messages, but Run does not forward them. The CLI owns its own
// context through --resume.
//
// Zero messages is an error, because the CLI cannot continue without
// input. To resume an earlier conversation, use [WithSessionID] with
// Run. If the caller cancels ctx, Run interrupts the turn and leaves the
// subprocess running. The next Run continues the same session.
func (a *Agent) Run(ctx context.Context, msgs ...ai.Message) *agent.Stream {
	return agent.NewStream(func(push func(agent.Event)) ([]ai.Message, error) {
		return a.runTurn(ctx, msgs, push)
	})
}

// Messages returns a copy of the current conversation history.
func (a *Agent) Messages() []ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.messages) == 0 {
		return nil
	}
	out := make([]ai.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

// SessionID returns the session ID captured from the subprocess.
func (a *Agent) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

// Close stops the subprocess and returns its exit error, if there is
// one. It waits for the stdout reader goroutine to finish. As a result,
// no events arrive after Close returns.
func (a *Agent) Close() error {
	a.mu.Lock()
	t := a.transport
	a.transport = nil
	readerDone := a.readerDone
	a.mu.Unlock()

	if t == nil {
		return nil
	}

	err := t.close()
	if readerDone != nil {
		<-readerDone
	}
	return err
}

// runTurn makes sure that the input is valid. It then starts the
// subprocess if necessary, writes one user line, and blocks until the
// turn completes. runTurn is the producer behind the stream of
// [Agent.Run].
func (a *Agent) runTurn(
	ctx context.Context,
	msgs []ai.Message,
	push func(agent.Event),
) ([]ai.Message, error) {
	if len(msgs) == 0 {
		return nil, errors.New(
			"claude: Run without messages is not supported in stream-json mode; " +
				"the CLI needs the turn on stdin",
		)
	}

	if err := validateSessionID(a.cfg.sessionID); err != nil {
		return nil, err
	}

	// The whole turn becomes one stdin line, because the CLI starts a
	// turn for every user line it reads.
	line, err := buildUserLine(msgs)
	if err != nil {
		return nil, err
	}

	turnCh := make(chan turnResult, 1)

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil, errors.New("claude: already running")
	}
	a.running = true
	a.messages = append(a.messages, msgs...)
	a.push = push
	a.turnDone = turnCh
	a.runCtx = ctx
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.push = nil
		a.turnDone = nil
		a.runCtx = nil
		a.mu.Unlock()
	}()

	if err := a.ensureTransport(ctx); err != nil {
		return nil, err
	}

	a.mu.Lock()
	t := a.transport
	a.mu.Unlock()
	if t == nil {
		return nil, errors.New("claude: agent is closed")
	}

	// Mark that readLoop owes an agent_start for this turn. readLoop
	// publishes it with the next batch of parser events, so agent_start
	// carries the session ID from the subprocess init line. The agent
	// does not echo back the user messages from the caller, because the
	// caller already has them.
	a.expectAgentStart.Store(true)

	if err := t.writeUserMessage(line); err != nil {
		a.expectAgentStart.Store(false)
		return nil, err
	}

	return a.awaitTurn(ctx, t, push, turnCh)
}

// awaitTurn blocks until the reader signals the end of the turn. It also
// returns if the subprocess exits or the caller cancels ctx. It then
// completes the turn. If the caller cancels ctx, awaitTurn asks the CLI
// to abort the turn in progress. The persistent subprocess keeps running
// for the next Run.
func (a *Agent) awaitTurn(
	ctx context.Context,
	t transportIface,
	push func(agent.Event),
	turnCh <-chan turnResult,
) ([]ai.Message, error) {
	var result turnResult

	select {
	case result = <-turnCh:
	case <-t.exited():
		if e := t.exitErr(); e != nil {
			result.err = e
		} else {
			result.err = errors.New("claude: subprocess exited before turn completed")
		}
	case <-ctx.Done():
		_ = t.interrupt()
		result.err = ctx.Err()
	}

	a.mu.Lock()
	if result.sessionID != "" {
		a.sessionID = result.sessionID
	}
	a.messages = append(a.messages, result.messages...)
	a.mu.Unlock()

	if result.err != nil {
		return result.messages, result.err
	}

	push(agent.Event{
		Type:     agent.EventAgentEnd,
		Messages: result.messages,
		Usage:    result.usage,
	})

	return result.messages, nil
}

// publish forwards an event to the stream of the active run. If no run
// is in progress, publish ignores the event. Late lines from an aborted
// turn are one example.
func (a *Agent) publish(evt agent.Event) {
	a.mu.Lock()
	push := a.push
	a.mu.Unlock()
	if push != nil {
		push(evt)
	}
}

// maybePublishAgentStart publishes [agent.EventAgentStart] if runTurn
// marked the bracket as owed for the current turn. readLoop calls it
// immediately before it publishes each parser event. As a result,
// agent_start always comes before the parser output of the turn.
// readLoop is the only goroutine that publishes for a turn.
func (a *Agent) maybePublishAgentStart() {
	if !a.expectAgentStart.CompareAndSwap(true, false) {
		return
	}
	a.mu.Lock()
	sid := a.sessionID
	a.mu.Unlock()
	a.publish(agent.Event{
		Type:      agent.EventAgentStart,
		SessionID: sid,
	})
}

// ensureTransport starts the subprocess and the reader goroutine on
// first use.
func (a *Agent) ensureTransport(ctx context.Context) error {
	a.mu.Lock()
	if a.transport != nil {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	t, err := a.newTransport(ctx, a.cfg)
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.transport = t
	a.readerDone = make(chan struct{})
	a.mu.Unlock()

	go a.readLoop(t, a.readerDone)

	return nil
}

const maxLineSize = 10 * 1024 * 1024 // 10 MB

// readLoop scans the NDJSON lines from the subprocess. It gives each
// line to a per-turn [parser] and publishes the events to the active
// run. On each `result` line, readLoop sends the accumulated state of
// the turn to the current turnDone channel.
func (a *Agent) readLoop(t transportIface, done chan struct{}) {
	defer close(done)

	scanner := bufio.NewScanner(t.stdout())
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	p := &parser{model: a.model}
	var turnSessionID string

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}

		if line.Type == "control_request" {
			// The CLI starts this request. It is can_use_tool when the
			// package starts the CLI with --permission-prompt-tool stdio.
			// The CLI blocks the tool call until this process writes the
			// response. An inline answer here therefore cannot deadlock
			// the turn.
			a.handleControlRequest(t, line)
			continue
		}

		if line.Type == "system" && line.Subtype == "init" {
			// The subprocess starts. Capture the session ID so that the
			// agent_start of the turn and SessionID() can carry it. The
			// session lifecycle belongs to the caller. It is not an event
			// here.
			if line.SessionID != "" {
				turnSessionID = line.SessionID
				a.mu.Lock()
				if a.sessionID == "" {
					a.sessionID = line.SessionID
				}
				a.mu.Unlock()
			}
			continue
		}

		events := p.handleLine(line)
		if len(events) > 0 {
			a.maybePublishAgentStart()
		}
		for _, evt := range events {
			a.publish(evt)
		}

		if line.Type == "result" {
			a.deliverTurn(turnResult{
				messages:  p.messages,
				usage:     p.usage,
				sessionID: turnSessionID,
				err:       p.err,
			})
			p = &parser{model: a.model}
			turnSessionID = ""
		}
	}

	// The scanner stopped, because the subprocess closed stdout.
	if err := scanner.Err(); err != nil {
		a.deliverTurn(turnResult{err: err})
	}
}

// canUseToolRequest is the payload of a can_use_tool control_request
// from the CLI. The CLI emits one before each tool call that its own
// permission policy asks about.
type canUseToolRequest struct {
	Subtype   string         `json:"subtype"`
	ToolName  string         `json:"tool_name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
}

// handleControlRequest answers a control_request line from the CLI. It
// handles only can_use_tool. The call runs through the registered
// [agent.HookBeforeTool] hooks. The rules match the Default agent: a
// hook error or a Deny blocks the call. handleControlRequest then writes
// the decision back as a control_response. It ignores unknown subtypes.
func (a *Agent) handleControlRequest(t transportIface, line rawLine) {
	var req canUseToolRequest
	if err := json.Unmarshal(line.Request, &req); err != nil {
		return
	}
	if req.Subtype != "can_use_tool" {
		return
	}

	a.mu.Lock()
	ctx := a.runCtx
	msgs := make([]ai.Message, len(a.messages))
	copy(msgs, a.messages)
	a.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	allow := true
	var reason string
	tc := ai.ToolCall{
		ID:        req.ToolUseID,
		Name:      req.ToolName,
		Arguments: req.Input,
	}
	for _, hook := range a.cfg.beforeTool {
		out, err := hook(ctx, &agent.HookInput{
			Event:    agent.HookBeforeTool,
			Messages: msgs,
			ToolCall: &tc,
		})
		if err != nil {
			allow, reason = false, err.Error()
			break
		}
		if out != nil && out.Deny {
			allow, reason = false, out.DenyReason
			break
		}
	}

	resp, err := buildPermissionResponse(line.RequestID, allow, req.Input, reason)
	if err != nil {
		return
	}
	_ = t.writeControlResponse(resp)
}

// deliverTurn forwards a turn result to the Run that waits for it, if
// there is one. The turnDone channel has a buffer of one, so the send
// never blocks. If no Run waits, deliverTurn ignores the result. This
// happens for an unexpected result line outside a turn, or for a result
// that arrives after the caller cancels ctx.
func (a *Agent) deliverTurn(result turnResult) {
	a.mu.Lock()
	ch := a.turnDone
	a.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- result:
	default:
	}
}
