package claude

import (
	"bufio"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Agent implements [agent.Agent] by delegating the entire agent loop
// to a Claude Code CLI subprocess. Each [Agent.Send] or [Agent.Continue]
// spawns a new `claude --print` process.
type Agent struct {
	cfg config

	// sendFn is the function that spawns a subprocess and returns stdout.
	// Defaults to Agent.send; overridden in tests.
	sendFn func(ctx context.Context, args sendArgs) (io.ReadCloser, func() error, error)

	mu        sync.Mutex
	running   bool
	sessionID string
	messages  []agent.Message
	err       error
}

var _ agent.Agent = (*Agent)(nil)

// New creates a new Claude CLI subprocess [Agent].
func New(opts ...Option) *Agent {
	cfg := config{
		cliPath: "claude",
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	msgs := make([]agent.Message, len(cfg.history))
	copy(msgs, cfg.history)

	a := &Agent{
		cfg:       cfg,
		sessionID: cfg.sessionID,
		messages:  msgs,
	}
	a.sendFn = a.send
	return a
}

// Send adds a user message and runs the subprocess.
func (a *Agent) Send(ctx context.Context, input string) *agent.EventStream {
	return a.run(ctx, input, false)
}

// SendMessages adds messages and runs the subprocess.
// Only the text from the last user message is sent as the prompt.
func (a *Agent) SendMessages(
	ctx context.Context,
	msgs ...agent.Message,
) *agent.EventStream {
	a.mu.Lock()
	a.messages = append(a.messages, msgs...)
	a.mu.Unlock()

	var prompt string
	for i := len(msgs) - 1; i >= 0; i-- {
		if lm, ok := agent.AsLLMMessage(msgs[i]); ok {
			if lm.Message.Role == ai.RoleUser {
				prompt = lm.Message.Text()
				break
			}
		}
	}

	return a.run(ctx, prompt, false)
}

// Continue resumes from the current session without adding new messages.
func (a *Agent) Continue(ctx context.Context) *agent.EventStream {
	a.mu.Lock()
	sid := a.sessionID
	a.mu.Unlock()

	if sid == "" {
		return agent.ErrStream(errors.New("claude: no session to resume"))
	}

	return a.run(ctx, "", true)
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

// IsRunning reports whether the subprocess is currently executing.
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
// Can be used with [WithSessionID] to resume across Agent instances.
func (a *Agent) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

// run is the core method. It acquires the mutex, spawns the subprocess,
// and returns an EventStream that pushes events as NDJSON lines arrive.
func (a *Agent) run(ctx context.Context, prompt string, resume bool) *agent.EventStream {
	a.mu.Lock()

	if a.running {
		a.mu.Unlock()
		return agent.ErrStream(errors.New("claude: already running"))
	}

	a.running = true
	a.err = nil

	var inputMsgs []agent.Message
	if prompt != "" && !resume {
		userMsg := agent.NewLLMMessage(ai.UserMessage(prompt))
		a.messages = append(a.messages, userMsg)
		inputMsgs = append(inputMsgs, userMsg)
	}

	sid := a.sessionID
	a.mu.Unlock()

	return agent.NewStream(func(push func(agent.Event)) {
		defer a.stop()

		push(agent.Event{Type: agent.EventAgentStart})

		for _, m := range inputMsgs {
			if lm, ok := agent.AsLLMMessage(m); ok {
				push(agent.Event{
					Type:    agent.EventMessageStart,
					Message: &lm.Message,
				})
				push(agent.Event{
					Type:    agent.EventMessageEnd,
					Message: &lm.Message,
				})
			}
		}

		args := sendArgs{
			prompt:    prompt,
			sessionID: sid,
			resume:    resume,
		}

		stdout, cleanup, err := a.sendFn(ctx, args)
		if err != nil {
			a.setErr(err)
			push(agent.Event{Type: agent.EventAgentEnd, Err: err})
			return
		}

		m, loopErr := a.processOutput(push, stdout)
		cleanupErr := cleanup()

		if loopErr == nil {
			loopErr = m.err
		}
		if loopErr == nil {
			loopErr = cleanupErr
		}

		a.mu.Lock()
		if m.sessionID != "" {
			a.sessionID = m.sessionID
		}
		for _, msg := range m.messages {
			a.messages = append(a.messages, agent.NewLLMMessage(msg))
		}
		a.err = loopErr
		a.mu.Unlock()

		push(agent.Event{
			Type:     agent.EventAgentEnd,
			Messages: m.messages,
			Usage:    m.usage,
			Err:      loopErr,
		})
	})
}

// stop marks the agent as no longer running.
func (a *Agent) stop() {
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
}

// setErr stores an error under the mutex.
func (a *Agent) setErr(err error) {
	a.mu.Lock()
	a.err = err
	a.running = false
	a.mu.Unlock()
}

const maxLineSize = 10 * 1024 * 1024 // 10MB

// processOutput reads NDJSON lines from the subprocess stdout and
// pushes agent events. Returns accumulated state (session ID, messages, usage).
func (a *Agent) processOutput(
	push func(agent.Event),
	stdout interface{ Read([]byte) (int, error) },
) (*parser, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	m := &parser{}

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}

		for _, evt := range m.handleLine(line) {
			push(evt)
		}
	}

	if err := scanner.Err(); err != nil {
		return m, err
	}

	return m, nil
}
