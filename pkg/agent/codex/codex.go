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
)

// Agent implements [agent.Agent]. Each turn goes to the Codex CLI. The
// first turn runs `codex exec --json`. After the CLI reports a thread ID,
// each later turn runs `codex exec resume --json <thread-id>`.
type Agent struct {
	cfg config

	// model is the [ai.Model] of the caller. The agent keeps it for the
	// Cost rates, because the CLI reports token counts but no cost per
	// category.
	model ai.Model

	runFn func(ctx context.Context, cfg config, args runArgs) (io.ReadCloser, func() error, error)

	mu        sync.Mutex
	running   bool
	messages  []ai.Message
	sessionID string
}

var _ agent.Agent = (*Agent)(nil)

// New creates a new Codex CLI [Agent] for model. To create an agent from
// a spec, register [Factory] with the catalog under the "codex" kind.
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
		model:     model,
		cfg:       cfg,
		runFn:     runCodex,
		messages:  msgs,
		sessionID: cfg.sessionID,
	}
}

// Run implements [agent.Agent]. It appends msgs to the history, sends the
// most recent user message to the Codex CLI, and runs one turn. It keeps
// the non-user messages locally and does not forward them. After the
// first turn, the CLI owns the context through its thread ID.
//
// Zero messages is an error, because the CLI cannot continue without
// input. A cancel of ctx kills the Codex child subprocess. The next Run
// starts a new turn and resumes the captured thread.
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

// Close implements [agent.Agent]. The Codex CLI runs one subprocess for
// each turn. Nothing stays open between runs, so Close releases nothing.
func (a *Agent) Close() error { return nil }

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

// runTurn makes sure that the input is valid. It then starts one
// `codex exec` subprocess and streams the output as events. runTurn is
// the producer behind the stream of [Agent.Run].
func (a *Agent) runTurn(
	ctx context.Context,
	msgs []ai.Message,
	push func(agent.Event),
) ([]ai.Message, error) {
	if len(msgs) == 0 {
		return nil, errors.New("codex: Run without messages is not supported; pass a user message to resume the thread")
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
		return nil, errors.New("codex: Run requires at least one user message")
	}

	userText := userMsg.Text()
	if userText == "" {
		return nil, errors.New("codex: user message has no text content")
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil, errors.New("codex: already running")
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
		Usage:    result.usage,
	})

	return result.messages, nil
}

const maxLineSize = 10 * 1024 * 1024 // 10MB

func (a *Agent) readTurn(stdout io.Reader, push func(agent.Event)) turnResult {
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

		case "turn.started":
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
			push(agent.Event{Type: agent.EventTurnStart})
			turnOpen = true

		case "item.started":
			switch line.Item.Type {
			case "command_execution":
				push(agent.Event{
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
				appendMessage(&result, msg, push)

			default:
				call, ok := line.Item.serverToolCall()
				if !ok {
					continue
				}
				push(agent.Event{
					Type:       agent.EventToolExecutionStart,
					ToolCallID: call.ID,
					ToolName:   string(call.ServerType),
					Args:       call.Arguments,
				})
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
				push(agent.Event{
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
				lastMessageIndex = appendMessage(&result, msg, push)

			case "todo_list":
				msg := ai.ToolResultMessage(
					line.Item.ID,
					"TodoWrite",
					ai.Text{Text: "Todos updated."},
				)
				appendMessage(&result, msg, push)

			case "plan_update":
				call := ai.ToolCall{
					ID:        line.Item.ID,
					Name:      "TodoWrite",
					Arguments: line.Item.todoArguments(),
				}
				msg := ai.AssistantMessage(call)
				msg.API = "codex-cli"
				msg.Model = a.cfg.model
				appendMessage(&result, msg, push)
				appendMessage(
					&result,
					ai.ToolResultMessage(
						line.Item.ID,
						"TodoWrite",
						ai.Text{Text: "Todos updated."},
					),
					push,
				)

			case "reasoning":
				if line.Item.Text == "" {
					continue
				}
				msg := ai.AssistantMessage(ai.Thinking{Thinking: line.Item.Text})
				msg.API = "codex-cli"
				msg.Model = a.cfg.model
				appendMessage(&result, msg, push)

			default:
				call, ok := line.Item.serverToolCall()
				if !ok {
					continue
				}
				push(agent.Event{
					Type:       agent.EventToolExecutionEnd,
					ToolCallID: call.ID,
					ToolName:   string(call.ServerType),
					Result:     call.Output.Content,
					IsError:    call.Output.IsError,
				})
			}

		case "turn.completed":
			if line.Usage != nil {
				result.usage = usageFromCodex(a.model, *line.Usage)
			}
			var lastMessage *ai.Message
			if lastMessageIndex >= 0 {
				result.messages[lastMessageIndex].Usage = result.usage
				lastMessage = &result.messages[lastMessageIndex]
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

		case "turn.failed", "error":
			result.err = line.error()
		}
	}

	if err := scanner.Err(); err != nil {
		result.err = err
	}

	return result
}

func appendMessage(result *turnResult, msg ai.Message, push func(agent.Event)) int {
	result.messages = append(result.messages, msg)
	index := len(result.messages) - 1
	message := &result.messages[index]
	push(agent.Event{
		Type:    agent.EventMessageStart,
		Message: message,
	})
	push(agent.Event{
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
