package codex

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

// Agent implements [agent.Agent] by delegating each turn to the Codex CLI.
// The first turn runs `codex exec --json`; subsequent turns run
// `codex exec resume --json <thread-id>` after the CLI reports a thread ID.
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

// New creates a new Codex CLI [Agent] for model. Register it for string-based
// creation with agent.RegisterAgent("codex", codex.New).
func New(model ai.Model, opts ...agent.Option) *Agent {
	return newFromConfig(model, agent.ApplyOptions(opts...))
}

func newFromConfig(model ai.Model, ac agent.Config) *Agent {
	cfg := config{
		cliPath:        "codex",
		approvalPolicy: "never",
	}
	if ext, ok := ac.Extensions[extensionKey].(*config); ok && ext != nil {
		cfg = *ext
		if cfg.cliPath == "" {
			cfg.cliPath = "codex"
		}
		if cfg.approvalPolicy == "" {
			cfg.approvalPolicy = "never"
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
		runFn:     runCodex,
		broker:    pubsub.NewBroker[agent.Event](pubsub.WithBlockingPublish()),
		messages:  msgs,
		sessionID: cfg.sessionID,
	}
}

// Send adds a user message and runs one Codex turn.
func (a *Agent) Send(ctx context.Context, input string) error {
	return a.SendMessages(ctx, ai.UserMessage(input))
}

// SendMessages appends messages to history and sends the most recent user
// message to the Codex CLI. Non-user messages are retained locally but not
// forwarded; after the first turn the CLI owns context through its thread ID.
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
		return errors.New("codex: SendMessages requires at least one user message")
	}

	userText := userMsg.Text()
	if userText == "" {
		return errors.New("codex: user message has no text content")
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("codex: already running")
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

// Abort cancels the in-flight turn, if one is active, terminating the Codex
// child subprocess. It is a no-op when the agent is idle. The next Send starts
// a fresh turn (resuming the captured thread). Mirrors pi-mono's agent.abort().
func (a *Agent) Abort() {
	a.mu.Lock()
	cancel := a.turnCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Continue is not supported by the Codex CLI agent. Use Send with a prompt
// to resume the captured thread.
func (a *Agent) Continue(ctx context.Context) error {
	return errors.New("codex: Continue is not supported; use Send to resume")
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

// SessionID returns the Codex thread ID captured from the subprocess.
func (a *Agent) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

type turnResult struct {
	messages []ai.Message
	usage    ai.Usage
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
		Usage:    result.usage,
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
		result           turnResult
		agentStarted     bool
		turnOpen         bool
		lastMessageIndex = -1
		toolResults      []ai.Message
	)

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}

		switch line.Type {
		case "thread.started":
			if line.ThreadID == "" {
				continue
			}
			a.mu.Lock()
			a.sessionID = line.ThreadID
			a.mu.Unlock()
			a.fireSessionInit(line.ThreadID)

		case "turn.started":
			if !agentStarted {
				a.publishAgentStart()
				agentStarted = true
			}
			a.broker.Publish(agent.Event{Type: agent.EventTurnStart})
			turnOpen = true

		case "item.started":
			switch line.Item.Type {
			case "command_execution":
				a.broker.Publish(agent.Event{
					Type:       agent.EventToolExecutionStart,
					ToolCallID: line.Item.ID,
					ToolName:   "bash",
					Args: map[string]any{
						"command": line.Item.Command,
					},
				})

			case "todo_list":
				msg := ai.AssistantMessage(ai.ToolCall{
					ID:        line.Item.ID,
					Name:      "TodoWrite",
					Arguments: line.Item.todoArguments(),
				})
				msg.API = "codex-cli"
				msg.Model = a.cfg.model
				a.appendMessage(&result, msg)
			}

		case "item.completed":
			switch line.Item.Type {
			case "command_execution":
				msg := ai.ToolResultMessage(
					line.Item.ID,
					"bash",
					ai.Text{Text: line.Item.AggregatedOutput},
				)
				msg.IsError = line.Item.commandFailed()
				toolResults = append(toolResults, msg)
				a.broker.Publish(agent.Event{
					Type:       agent.EventToolExecutionEnd,
					ToolCallID: line.Item.ID,
					ToolName:   "bash",
					Result:     line.Item.AggregatedOutput,
					IsError:    msg.IsError,
				})

			case "agent_message":
				msg := ai.AssistantMessage(ai.Text{Text: line.Item.Text})
				msg.API = "codex-cli"
				msg.Model = a.cfg.model
				msg.StopReason = ai.StopReasonStop
				lastMessageIndex = a.appendMessage(&result, msg)

			case "todo_list":
				msg := ai.ToolResultMessage(
					line.Item.ID,
					"TodoWrite",
					ai.Text{Text: "Todos updated."},
				)
				a.appendMessage(&result, msg)
			}

		case "turn.completed":
			if line.Usage != nil {
				result.usage = usageFromCodex(*line.Usage)
			}
			var lastMessage *ai.Message
			if lastMessageIndex >= 0 {
				result.messages[lastMessageIndex].Usage = result.usage
				lastMessage = &result.messages[lastMessageIndex]
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

		case "turn.failed", "error":
			result.err = line.error()
		}
	}

	if err := scanner.Err(); err != nil {
		result.err = err
	}

	return result
}

func (a *Agent) appendMessage(result *turnResult, msg ai.Message) int {
	result.messages = append(result.messages, msg)
	index := len(result.messages) - 1
	message := &result.messages[index]
	a.broker.Publish(agent.Event{
		Type:    agent.EventMessageStart,
		Message: message,
	})
	a.broker.Publish(agent.Event{
		Type:    agent.EventMessageEnd,
		Message: message,
	})
	return index
}

func promptText(system, user string) string {
	if system == "" {
		return user
	}
	return strings.TrimSpace(system) + "\n\n" + user
}
