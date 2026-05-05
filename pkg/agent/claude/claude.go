package claude

import (
	"bufio"
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/pubsub"
)

// Agent implements [agent.Agent] by delegating the entire agent loop
// to a long-lived Claude Code CLI subprocess. The subprocess is started
// lazily on first [Agent.Send] with `--input-format stream-json` and
// serves many turns: each Send writes one SDKUserMessage to stdin and
// blocks until the CLI emits the corresponding result line.
type Agent struct {
	cfg config

	// newTransport is the factory for the CLI subprocess. Overridden in tests.
	newTransport func(ctx context.Context, cfg config) (transportIface, error)

	broker *pubsub.Broker[agent.Event]

	mu        sync.Mutex
	running   bool
	done      chan struct{}
	lastMsgs  []ai.Message
	sessionID string
	messages  []agent.Message
	err       error

	// session lifecycle tracking — session_init fires once when the
	// subprocess emits its system/init line; session_end fires once
	// from Close (and only if session_init fired).
	sessionInitOnce  sync.Once
	sessionEndOnce   sync.Once
	sessionInitFired bool

	// expectAgentStart signals the readLoop to publish [agent.EventAgentStart]
	// just before its next batch of parser events. runTurn sets this true
	// before writing the user line; readLoop atomically clears it when it
	// publishes the bracket. Routing the publish through readLoop keeps
	// agent_start ordered ahead of every per-turn parser event from the
	// same subprocess line.
	expectAgentStart atomic.Bool

	// transport is the active subprocess, created on first Send.
	transport transportIface
	// readerDone closes when the stdout reader goroutine exits.
	readerDone chan struct{}
	// turnDone receives the end-of-turn signal from the reader for each Send.
	// Set by run() before writing and consumed before returning from the
	// background goroutine.
	turnDone chan turnResult
}

// turnResult carries per-turn accumulated state from the reader to the
// Send goroutine.
type turnResult struct {
	messages  []ai.Message
	usage     ai.Usage
	sessionID string
	err       error
}

var _ agent.Agent = (*Agent)(nil)

// Factory is the [agent.Factory] for the Claude CLI agent. Register it
// once at startup with [agent.RegisterFactory]:
//
//	agent.RegisterFactory("claude", claude.Factory)
//
// Callers then construct a Claude agent via [agent.GetFactory]. The
// factory consumes [agent.WithModelName] (string only — the CLI owns
// its model catalog) and any claude-specific options such as
// [WithCLIPath], [WithAllowedTools], or [WithSessionID].
var Factory agent.Factory = func(opts ...agent.Option) agent.Agent {
	return newFromConfig(agent.ApplyOptions(opts...))
}

// New creates a new Claude CLI subprocess [Agent] from [agent.Option]
// values. Prefer constructing via [Factory] when using the registry.
func New(opts ...agent.Option) *Agent {
	return newFromConfig(agent.ApplyOptions(opts...))
}

// newFromConfig builds an *Agent from a resolved [agent.Config]. Agent-level
// fields ([agent.Config.Model.Name], [agent.Config.MaxTurns],
// [agent.Config.History]) are mapped onto the claude-local [config], which
// is otherwise populated from [agent.Config.Extensions] under [extensionKey].
func newFromConfig(ac agent.Config) *Agent {
	cfg := config{cliPath: "claude"}
	if ext, ok := ac.Extensions[extensionKey].(*config); ok && ext != nil {
		cfg = *ext
		if cfg.cliPath == "" {
			cfg.cliPath = "claude"
		}
	}
	if ac.Model.Name != "" {
		cfg.model = ac.Model.Name
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

	var msgs []agent.Message
	if len(cfg.history) > 0 {
		msgs = make([]agent.Message, len(cfg.history))
		copy(msgs, cfg.history)
	}

	return &Agent{
		cfg:          cfg,
		newTransport: newTransport,
		broker:       pubsub.NewBroker[agent.Event](pubsub.WithBlockingPublish()),
		sessionID:    cfg.sessionID,
		messages:     msgs,
	}
}

// Send adds a user message and runs one turn on the persistent subprocess.
func (a *Agent) Send(ctx context.Context, input string) error {
	return a.sendUser(ctx, ai.UserMessage(input))
}

// SendMessages appends messages to history and sends the most recent user
// message (with its full content blocks) through the subprocess. Non-user
// messages are retained in history but not forwarded — the CLI owns its
// own context via --resume.
//
// The call is validated before any state mutation: if msgs contains no
// [agent.LLMMessage] with role=user, an error is returned synchronously
// and no events are published, matching the entry-point validation
// convention shared with [agent.Default].
func (a *Agent) SendMessages(
	ctx context.Context,
	msgs ...agent.Message,
) error {
	var userMsg *ai.Message
	for i := len(msgs) - 1; i >= 0; i-- {
		lm, ok := agent.AsLLMMessage(msgs[i])
		if !ok || lm.Message.Role != ai.RoleUser {
			continue
		}
		m := lm.Message
		userMsg = &m
		break
	}
	if userMsg == nil {
		return errors.New("claude: SendMessages requires at least one user message")
	}

	a.mu.Lock()
	a.messages = append(a.messages, msgs...)
	a.mu.Unlock()

	return a.runTurn(ctx, *userMsg)
}

// Continue is not supported in stream-json mode. Use [WithSessionID]
// combined with [Agent.Send] to resume a prior conversation.
func (a *Agent) Continue(ctx context.Context) error {
	return errors.New(
		"claude: Continue is not supported in stream-json mode; " +
			"use WithSessionID + Send to resume",
	)
}

// Subscribe returns a channel of agent events. Each call creates an
// independent subscription. Use [pubsub.After] to replay buffered events.
func (a *Agent) Subscribe(
	ctx context.Context,
	opts ...pubsub.SubscribeOption,
) <-chan pubsub.Event[agent.Event] {
	return a.broker.Subscribe(ctx, opts...)
}

// Wait blocks until the current turn completes and returns all new
// messages produced during that turn.
func (a *Agent) Wait(ctx context.Context) ([]ai.Message, error) {
	a.mu.Lock()
	if !a.running {
		msgs, err := a.lastMsgs, a.err
		a.mu.Unlock()
		return msgs, err
	}
	done := a.done
	a.mu.Unlock()

	select {
	case <-done:
		a.mu.Lock()
		msgs, err := a.lastMsgs, a.err
		a.mu.Unlock()
		return msgs, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close shuts down the subprocess and the event broker. Subsequent calls
// to [Agent.Subscribe] return closed channels. If the subprocess emitted
// its `system/init` line during the agent's lifetime
// ([agent.EventSessionInit] fired), Close emits a matching
// [agent.EventSessionEnd] before shutting down the broker, carrying any
// transport exit error.
func (a *Agent) Close() {
	a.mu.Lock()
	t := a.transport
	a.transport = nil
	readerDone := a.readerDone
	a.mu.Unlock()

	if t != nil {
		_ = t.close()
	}
	if readerDone != nil {
		<-readerDone
	}

	if a.sessionInitFired {
		a.sessionEndOnce.Do(func() {
			var exitErr error
			if t != nil {
				exitErr = t.exitErr()
			}
			a.broker.Publish(agent.Event{
				Type: agent.EventSessionEnd,
				Err:  exitErr,
			})
		})
	}

	a.broker.Shutdown()
}

// fireSessionInit emits [agent.EventSessionInit] on the very first
// `system/init` line and is a no-op on subsequent calls.
func (a *Agent) fireSessionInit(sid string) {
	a.sessionInitOnce.Do(func() {
		a.sessionInitFired = true
		a.broker.Publish(agent.Event{
			Type:      agent.EventSessionInit,
			SessionID: sid,
		})
	})
}

// maybePublishAgentStart publishes [agent.EventAgentStart] if runTurn
// flagged that the bracket is owed for the current turn. It is called
// from readLoop just before publishing each parser event so that
// agent_start is always serialized ahead of the turn's parser output
// — the sole publishing goroutine for a turn is readLoop.
func (a *Agent) maybePublishAgentStart() {
	if !a.expectAgentStart.CompareAndSwap(true, false) {
		return
	}
	a.mu.Lock()
	sid := a.sessionID
	a.mu.Unlock()
	a.broker.Publish(agent.Event{
		Type:      agent.EventAgentStart,
		SessionID: sid,
	})
}

// Messages returns a copy of the current conversation history.
func (a *Agent) Messages() []agent.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.messages) == 0 {
		return nil
	}
	out := make([]agent.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

// IsRunning reports whether a turn is currently executing.
func (a *Agent) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// Err returns the last error encountered, or nil.
func (a *Agent) Err() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// SessionID returns the session ID captured from the subprocess.
func (a *Agent) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

// sendUser wraps a text input into a user message and runs one turn.
func (a *Agent) sendUser(ctx context.Context, msg ai.Message) error {
	a.mu.Lock()
	a.messages = append(a.messages, agent.NewLLMMessage(msg))
	a.mu.Unlock()

	return a.runTurn(ctx, msg)
}

// runTurn starts the subprocess if needed, writes one user line, and spawns
// a goroutine that waits for the turn-end signal from the reader. Callers
// must have already appended the user message to [Agent.messages].
func (a *Agent) runTurn(ctx context.Context, userMsg ai.Message) error {
	line, err := buildUserLine(userMsg)
	if err != nil {
		return err
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("claude: already running")
	}

	a.running = true
	a.err = nil
	a.lastMsgs = nil
	a.done = make(chan struct{})
	turnCh := make(chan turnResult, 1)
	a.turnDone = turnCh
	a.mu.Unlock()

	if err := a.ensureTransport(ctx); err != nil {
		a.finishTurn(turnResult{err: err})
		return nil
	}

	// Mark that readLoop owes an agent_start for this turn. readLoop
	// publishes it inline with the next batch of parser events so
	// agent_start is guaranteed to precede any per-turn parser output
	// (and, on the first turn, to follow the session_init that the
	// init line produced). Caller-supplied user messages are not
	// echoed back — the caller already has them.
	a.expectAgentStart.Store(true)

	if err := a.transport.writeUserMessage(line); err != nil {
		a.expectAgentStart.Store(false)
		a.finishTurn(turnResult{err: err})
		return nil
	}

	go a.awaitTurn(ctx, turnCh)

	return nil
}

// awaitTurn blocks until the reader signals turn-end (or the subprocess dies
// or ctx is cancelled), then finalizes the turn.
func (a *Agent) awaitTurn(ctx context.Context, turnCh <-chan turnResult) {
	a.mu.Lock()
	t := a.transport
	a.mu.Unlock()

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
		result.err = ctx.Err()
	}

	a.finishTurn(result)
}

// finishTurn updates agent state, appends new messages to history, and
// publishes EventAgentEnd.
func (a *Agent) finishTurn(result turnResult) {
	a.mu.Lock()
	if result.sessionID != "" {
		a.sessionID = result.sessionID
	}
	for _, msg := range result.messages {
		a.messages = append(a.messages, agent.NewLLMMessage(msg))
	}
	a.lastMsgs = result.messages
	a.err = result.err
	a.turnDone = nil
	a.running = false
	done := a.done
	a.mu.Unlock()

	a.broker.Publish(agent.Event{
		Type:     agent.EventAgentEnd,
		Messages: result.messages,
		Usage:    result.usage,
		Err:      result.err,
	})

	if done != nil {
		close(done)
	}
}

// ensureTransport lazily starts the subprocess and reader goroutine.
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

// readLoop scans NDJSON lines from the subprocess, feeds them through a
// per-turn [parser], and publishes events. On each `result` line, the
// accumulated per-turn state is sent to the current turnDone channel.
func (a *Agent) readLoop(t transportIface, done chan struct{}) {
	defer close(done)

	scanner := bufio.NewScanner(t.stdout())
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	p := &parser{}
	var turnSessionID string

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}

		if line.Type == "system" && line.Subtype == "init" {
			if line.SessionID != "" {
				turnSessionID = line.SessionID
				a.mu.Lock()
				if a.sessionID == "" {
					a.sessionID = line.SessionID
				}
				a.mu.Unlock()
			}
			// Subprocess startup → session_init (once per subprocess
			// lifetime). Per-run brackets are emitted separately by
			// runTurn as agent_start / agent_end.
			a.fireSessionInit(line.SessionID)
			continue
		}

		events := p.handleLine(line)
		if len(events) > 0 {
			a.maybePublishAgentStart()
		}
		for _, evt := range events {
			a.broker.Publish(evt)
		}

		if line.Type == "result" {
			a.deliverTurn(turnResult{
				messages:  p.messages,
				usage:     p.usage,
				sessionID: turnSessionID,
				err:       p.err,
			})
			p = &parser{}
			turnSessionID = ""
		}
	}

	// Scanner exited — subprocess closed stdout.
	if err := scanner.Err(); err != nil {
		a.deliverTurn(turnResult{err: err})
	}
}

// deliverTurn forwards a turn result to the currently-waiting Send, if any.
// The turnDone channel is buffered size 1 so the send never blocks; if no
// Send is waiting the result is simply dropped (e.g. a spurious result line
// outside of a turn, or a result arriving after ctx cancellation).
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
