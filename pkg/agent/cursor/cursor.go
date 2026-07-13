package cursor

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/pubsub"
)

// Agent implements [agent.Agent] by delegating each turn to Cursor Agent CLI.
type Agent struct {
	cfg config

	runFn func(ctx context.Context, cfg config, args runArgs) (io.ReadCloser, func() error, error)

	broker *pubsub.Broker[agent.Event]

	mu         sync.Mutex
	running    bool
	done       chan struct{}
	turnCancel context.CancelFunc
	lastMsgs   []ai.Message
	messages   []ai.Message
	err        error
	sessionID  string

	sessionInitOnce  sync.Once
	sessionEndOnce   sync.Once
	sessionInitFired bool
}

var _ agent.Agent = (*Agent)(nil)

// New creates a new Cursor CLI [Agent] for model. Register it for string-based
// creation with agent.RegisterAgent("cursor", cursor.New).
func New(model ai.Model, opts ...agent.Option) *Agent {
	return newFromConfig(model, agent.ApplyOptions(opts...))
}

func newFromConfig(model ai.Model, ac agent.Config) *Agent {
	cfg := config{
		cliPath: "cursor-agent",
	}
	if ext, ok := ac.Extensions[extensionKey].(*config); ok && ext != nil {
		cfg = *ext
		if cfg.cliPath == "" {
			cfg.cliPath = "cursor-agent"
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

	msgs := make([]ai.Message, len(cfg.history))
	copy(msgs, cfg.history)

	return &Agent{
		cfg:       cfg,
		runFn:     runCursor,
		broker:    pubsub.NewBroker[agent.Event](pubsub.WithBlockingPublish()),
		messages:  msgs,
		sessionID: cfg.sessionID,
	}
}

// Send adds a user message and runs one Cursor turn.
func (a *Agent) Send(ctx context.Context, input string) error {
	return a.SendMessages(ctx, ai.UserMessage(input))
}

// SendMessages appends messages to history and sends the most recent user
// message to Cursor CLI. Non-user messages are retained locally but not
// forwarded; after the first turn the CLI owns context through its session ID.
func (a *Agent) SendMessages(ctx context.Context, msgs ...ai.Message) error {
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
		return errors.New("cursor: SendMessages requires at least one user message")
	}

	userText := userMsg.Text()
	if userText == "" {
		return errors.New("cursor: user message has no text content")
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("cursor: already running")
	}

	// Own a turn-scoped context so [Agent.Abort] can cancel the in-flight
	// turn (the transport kills the child on ctx.Done) without the caller
	// holding the handle — mirroring pi-mono's per-run AbortController.
	turnCtx, turnCancel := context.WithCancel(ctx)

	a.running = true
	a.err = nil
	a.lastMsgs = nil
	a.done = make(chan struct{})
	a.turnCancel = turnCancel
	a.messages = append(a.messages, msgs...)

	sessionID := a.sessionID
	args := runArgs{
		prompt:    promptText(a.cfg.systemPrompt, userText),
		resume:    sessionID != "",
		sessionID: sessionID,
	}
	a.mu.Unlock()

	go a.runTurn(turnCtx, args)

	return nil
}

// Abort cancels the in-flight turn, if one is active, terminating the Cursor
// child subprocess. It is a no-op when the agent is idle. The next Send starts
// a fresh turn (resuming the captured chat). Mirrors pi-mono's agent.abort().
func (a *Agent) Abort() {
	a.mu.Lock()
	cancel := a.turnCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Continue is not supported by the Cursor CLI agent. Use Send with a prompt
// to resume the captured session.
func (a *Agent) Continue(ctx context.Context) error {
	return errors.New("cursor: Continue is not supported; use Send to resume")
}

// Subscribe returns a channel of agent events.
func (a *Agent) Subscribe(
	ctx context.Context,
	opts ...pubsub.SubscribeOption,
) <-chan pubsub.Event[agent.Event] {
	return a.broker.Subscribe(ctx, opts...)
}

// Wait blocks until the current turn completes and returns all new messages
// produced during that turn.
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

// Close waits for any active turn, emits session_end if needed, and shuts
// down the event broker.
func (a *Agent) Close() {
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()
	if done != nil {
		<-done
	}
	if a.sessionInitFired {
		a.sessionEndOnce.Do(func() {
			a.broker.Publish(agent.Event{Type: agent.EventSessionEnd})
		})
	}
	a.broker.Shutdown()
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

// SessionID returns the Cursor chat session ID captured from the subprocess.
func (a *Agent) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

type turnResult struct {
	messages []ai.Message
	err      error
}

func (a *Agent) runTurn(ctx context.Context, args runArgs) {
	stdout, cleanup, err := a.runFn(ctx, a.cfg, args)
	if err != nil {
		a.finishTurn(turnResult{err: err})
		return
	}

	result := a.readTurn(stdout)
	cleanupErr := cleanup()
	if result.err == nil {
		result.err = cleanupErr
	}

	a.finishTurn(result)
}

func (a *Agent) finishTurn(result turnResult) {
	a.mu.Lock()
	a.messages = append(a.messages, result.messages...)
	a.lastMsgs = result.messages
	a.err = result.err
	a.running = false
	cancel := a.turnCancel
	a.turnCancel = nil
	done := a.done
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	a.broker.Publish(agent.Event{
		Type:     agent.EventAgentEnd,
		Messages: result.messages,
		Err:      result.err,
	})

	if done != nil {
		close(done)
	}
}

func (a *Agent) fireSessionInit(sid string) {
	a.sessionInitOnce.Do(func() {
		a.sessionInitFired = true
		a.broker.Publish(agent.Event{
			Type:      agent.EventSessionInit,
			SessionID: sid,
		})
	})
}

func (a *Agent) publishAgentStart() {
	a.mu.Lock()
	sid := a.sessionID
	a.mu.Unlock()
	if !a.sessionInitFired && sid != "" {
		a.fireSessionInit(sid)
	}
	a.broker.Publish(agent.Event{
		Type:      agent.EventAgentStart,
		SessionID: sid,
	})
}

const maxLineSize = 10 * 1024 * 1024 // 10MB

func (a *Agent) readTurn(stdout io.Reader) turnResult {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var (
		result       turnResult
		agentStarted bool
		turnOpen     bool
		messageSent  bool
		assistant    strings.Builder
		lastMessage  *ai.Message
		toolResults  []ai.Message
	)

	ensureTurn := func() {
		if !agentStarted {
			a.publishAgentStart()
			agentStarted = true
		}
		if !turnOpen {
			a.broker.Publish(agent.Event{Type: agent.EventTurnStart})
			turnOpen = true
		}
	}

	emitMessage := func(text string) {
		if text == "" || messageSent {
			return
		}
		ensureTurn()
		msg := ai.AssistantMessage(ai.Text{Text: text})
		msg.API = "cursor-cli"
		msg.Model = a.cfg.model
		msg.StopReason = ai.StopReasonStop
		result.messages = append(result.messages, msg)
		lastMessage = &result.messages[len(result.messages)-1]
		a.broker.Publish(agent.Event{
			Type:    agent.EventMessageStart,
			Message: lastMessage,
		})
		a.broker.Publish(agent.Event{
			Type:    agent.EventMessageEnd,
			Message: lastMessage,
		})
		messageSent = true
	}

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}

		switch line.Type {
		case "system":
			if line.Subtype != "init" || line.SessionID == "" {
				continue
			}
			a.mu.Lock()
			a.sessionID = line.SessionID
			a.mu.Unlock()
			a.fireSessionInit(line.SessionID)

		case "assistant":
			text := line.Message.text()
			if text == "" {
				continue
			}
			ensureTurn()
			assistant.WriteString(text)

		case "tool_call":
			ensureTurn()
			info, ok := line.toolInfo()
			if !ok {
				continue
			}
			switch line.Subtype {
			case "started":
				a.broker.Publish(agent.Event{
					Type:       agent.EventToolExecutionStart,
					ToolCallID: info.ID,
					ToolName:   info.Name,
					Args:       info.Args,
				})
			case "completed":
				msg := ai.ToolResultMessage(
					info.ID,
					info.Name,
					ai.Text{Text: info.Result},
				)
				msg.IsError = info.IsError
				toolResults = append(toolResults, msg)
				a.broker.Publish(agent.Event{
					Type:       agent.EventToolExecutionEnd,
					ToolCallID: info.ID,
					ToolName:   info.Name,
					Result:     info.Result,
					IsError:    info.IsError,
				})
			}

		case "result":
			if line.SessionID != "" {
				a.mu.Lock()
				a.sessionID = line.SessionID
				a.mu.Unlock()
			}
			text := assistant.String()
			if text == "" {
				text = line.Result
			}
			emitMessage(text)
			if line.IsError {
				result.err = line.err()
			}
			if turnOpen {
				a.broker.Publish(agent.Event{
					Type:        agent.EventTurnEnd,
					Message:     lastMessage,
					ToolResults: toolResults,
				})
				turnOpen = false
				toolResults = nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		result.err = err
	}
	if !messageSent && assistant.Len() > 0 {
		emitMessage(assistant.String())
	}
	if turnOpen {
		a.broker.Publish(agent.Event{
			Type:        agent.EventTurnEnd,
			Message:     lastMessage,
			ToolResults: toolResults,
		})
	}

	return result
}

func promptText(system, user string) string {
	if system == "" {
		return user
	}
	return strings.TrimSpace(system) + "\n\n" + user
}
