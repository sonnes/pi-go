package claude

import (
	"bufio"
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Agent implements [agent.Agent] by delegating the entire agent loop
// to a long-lived Claude Code CLI subprocess. The subprocess is started
// lazily on the first [Agent.Run] with `--input-format stream-json` and
// serves many turns: each Run writes one SDKUserMessage to stdin and
// completes when the CLI emits the corresponding result line.
type Agent struct {
	cfg config

	// newTransport is the factory for the CLI subprocess. Overridden in tests.
	newTransport func(ctx context.Context, cfg config) (transportIface, error)

	mu        sync.Mutex
	running   bool
	sessionID string
	messages  []ai.Message
	// push is the active run's event sink; nil when idle. Events
	// arriving between runs are dropped.
	push func(agent.Event)
	// turnDone receives the end-of-turn signal from the reader for the
	// active run.
	turnDone chan turnResult

	// expectAgentStart signals the readLoop to publish [agent.EventAgentStart]
	// just before its next batch of parser events. runTurn sets this true
	// before writing the user line; readLoop atomically clears it when it
	// publishes the bracket. Routing the publish through readLoop keeps
	// agent_start carrying the session ID captured from the subprocess
	// `system/init` line, which on a fresh subprocess arrives after the
	// user line is written.
	expectAgentStart atomic.Bool

	// transport is the active subprocess, created on first Run.
	transport transportIface
	// readerDone closes when the stdout reader goroutine exits.
	readerDone chan struct{}
}

// turnResult carries per-turn accumulated state from the reader to the
// Run producer goroutine.
type turnResult struct {
	messages  []ai.Message
	usage     ai.Usage
	sessionID string
	err       error
}

var _ agent.Agent = (*Agent)(nil)

// New creates a new Claude CLI subprocess [Agent] for model. For spec-based
// creation, wrap it in an [agent.Factory] and register it with the catalog
// under the "claude" kind. The CLI owns its model catalog, so it uses the
// model's name/ID plus any claude-specific options such as [WithCLIPath],
// [WithAllowedTools], or [WithSessionID].
func New(model ai.Model, opts ...agent.Option) *Agent {
	return newFromConfig(model, agent.ApplyOptions(opts...))
}

// newFromConfig builds an *Agent from a resolved [agent.Config]. Agent-level
// fields ([agent.Config.Model.Name], [agent.Config.MaxTurns],
// [agent.Config.History]) are mapped onto the claude-local [config], which
// is otherwise populated from [agent.Config.Extensions] under [extensionKey].
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

	var msgs []ai.Message
	if len(cfg.history) > 0 {
		msgs = make([]ai.Message, len(cfg.history))
		copy(msgs, cfg.history)
	}

	return &Agent{
		cfg:          cfg,
		newTransport: newTransport,
		sessionID:    cfg.sessionID,
		messages:     msgs,
	}
}

// Run implements [agent.Agent]. It appends msgs to history, sends the
// most recent user message (with its full content blocks) through the
// subprocess, and runs one turn. Non-user messages are retained in
// history but not forwarded — the CLI owns its own context via
// --resume.
//
// Zero messages is an error: the CLI cannot continue without input.
// Use [WithSessionID] + Run to resume a prior conversation. Canceling
// ctx interrupts the turn while leaving the subprocess running, so the
// next Run continues the same session.
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

// Close shuts down the subprocess and returns its exit error, if any.
// It waits for the stdout reader goroutine to finish so no events are
// delivered after Close returns.
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

// runTurn validates the input, starts the subprocess if needed, writes
// one user line, and blocks until the turn completes. It is the
// producer behind [Agent.Run]'s stream.
func (a *Agent) runTurn(
	ctx context.Context,
	msgs []ai.Message,
	push func(agent.Event),
) ([]ai.Message, error) {
	if len(msgs) == 0 {
		return nil, errors.New(
			"claude: Run without messages is not supported in stream-json mode; " +
				"use WithSessionID + Run to resume",
		)
	}

	var userMsg *ai.Message
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != ai.RoleUser {
			continue
		}
		m := msgs[i]
		userMsg = &m
		break
	}
	if userMsg == nil {
		return nil, errors.New("claude: Run requires at least one user message")
	}

	line, err := buildUserLine(*userMsg)
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
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.push = nil
		a.turnDone = nil
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
	// publishes it inline with the next batch of parser events so
	// agent_start carries the session ID from the subprocess init line.
	// Caller-supplied user messages are not echoed back — the caller
	// already has them.
	a.expectAgentStart.Store(true)

	if err := t.writeUserMessage(line); err != nil {
		a.expectAgentStart.Store(false)
		return nil, err
	}

	return a.awaitTurn(ctx, t, push, turnCh)
}

// awaitTurn blocks until the reader signals turn-end (or the subprocess
// dies or ctx is cancelled), then finalizes the turn. On ctx
// cancellation it asks the CLI to abort the in-flight turn while
// leaving the persistent subprocess running for the next Run.
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

// publish forwards an event to the active run's stream, dropping it
// when no run is in flight (e.g. late lines from an aborted turn).
func (a *Agent) publish(evt agent.Event) {
	a.mu.Lock()
	push := a.push
	a.mu.Unlock()
	if push != nil {
		push(evt)
	}
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
	a.publish(agent.Event{
		Type:      agent.EventAgentStart,
		SessionID: sid,
	})
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
// per-turn [parser], and publishes events to the active run. On each
// `result` line, the accumulated per-turn state is sent to the current
// turnDone channel.
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
			// Subprocess startup — capture the session ID so the turn's
			// agent_start (and SessionID()) can carry it. Session
			// lifecycle is the caller's concern, not an event here.
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
			p = &parser{}
			turnSessionID = ""
		}
	}

	// Scanner exited — subprocess closed stdout.
	if err := scanner.Err(); err != nil {
		a.deliverTurn(turnResult{err: err})
	}
}

// deliverTurn forwards a turn result to the currently-waiting Run, if any.
// The turnDone channel is buffered size 1 so the send never blocks; if no
// Run is waiting the result is simply dropped (e.g. a spurious result line
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
