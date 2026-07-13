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
)

// Agent implements [agent.Agent] by delegating each turn to Cursor Agent CLI.
type Agent struct {
	cfg config

	runFn func(ctx context.Context, cfg config, args runArgs) (io.ReadCloser, func() error, error)

	mu        sync.Mutex
	running   bool
	messages  []ai.Message
	sessionID string
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
		messages:  msgs,
		sessionID: cfg.sessionID,
	}
}

// Run implements [agent.Agent]. It appends msgs to history, sends the
// most recent user message to Cursor CLI, and runs one turn. Non-user
// messages are retained locally but not forwarded; after the first turn
// the CLI owns context through its session ID.
//
// Zero messages is an error: the CLI cannot continue without input.
// Canceling ctx kills the Cursor child subprocess; the next Run starts
// a fresh turn resuming the captured chat.
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

// Close implements [agent.Agent]. The Cursor CLI runs one subprocess
// per turn, so there is nothing held between runs to release.
func (a *Agent) Close() error { return nil }

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

// runTurn validates the input, spawns one Cursor subprocess, and
// streams its output as events. It is the producer behind
// [Agent.Run]'s stream.
func (a *Agent) runTurn(
	ctx context.Context,
	msgs []ai.Message,
	push func(agent.Event),
) ([]ai.Message, error) {
	if len(msgs) == 0 {
		return nil, errors.New("cursor: Run without messages is not supported; pass a user message to resume the chat")
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
		return nil, errors.New("cursor: Run requires at least one user message")
	}

	userText := userMsg.Text()
	if userText == "" {
		return nil, errors.New("cursor: user message has no text content")
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil, errors.New("cursor: already running")
	}
	a.running = true
	a.messages = append(a.messages, msgs...)
	sessionID := a.sessionID
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	args := runArgs{
		prompt:    promptText(a.cfg.systemPrompt, userText),
		resume:    sessionID != "",
		sessionID: sessionID,
	}

	stdout, cleanup, err := a.runFn(ctx, a.cfg, args)
	if err != nil {
		return nil, err
	}

	result := a.readTurn(stdout, push)
	cleanupErr := cleanup()
	if result.err == nil {
		result.err = cleanupErr
	}

	a.mu.Lock()
	a.messages = append(a.messages, result.messages...)
	a.mu.Unlock()

	if result.err != nil {
		return result.messages, result.err
	}

	push(agent.Event{
		Type:     agent.EventAgentEnd,
		Messages: result.messages,
	})

	return result.messages, nil
}

const maxLineSize = 10 * 1024 * 1024 // 10MB

func (a *Agent) readTurn(stdout io.Reader, push func(agent.Event)) turnResult {
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
			a.mu.Lock()
			sid := a.sessionID
			a.mu.Unlock()
			push(agent.Event{
				Type:      agent.EventAgentStart,
				SessionID: sid,
			})
			agentStarted = true
		}
		if !turnOpen {
			push(agent.Event{Type: agent.EventTurnStart})
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
		push(agent.Event{
			Type:    agent.EventMessageStart,
			Message: lastMessage,
		})
		push(agent.Event{
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
				push(agent.Event{
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
				push(agent.Event{
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
				push(agent.Event{
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
		push(agent.Event{
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
