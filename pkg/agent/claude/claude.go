package claude

import (
	"bufio"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/pubsub"
)

// Agent implements [agent.Agent] by delegating the entire agent loop
// to a Claude Code CLI subprocess. Each [Agent.Send] or [Agent.Continue]
// spawns a new `claude --print` process.
type Agent struct {
	cfg config

	// sendFn is the function that spawns a subprocess and returns stdout.
	// Defaults to Agent.send; overridden in tests.
	sendFn func(ctx context.Context, args sendArgs) (io.ReadCloser, func() error, error)

	broker *pubsub.Broker[agent.Event]

	mu        sync.Mutex
	running   bool
	done      chan struct{}
	lastMsgs  []ai.Message
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
		broker:    pubsub.NewBroker[agent.Event](pubsub.WithBlockingPublish()),
		sessionID: cfg.sessionID,
		messages:  msgs,
	}
	a.sendFn = a.send
	return a
}

// Send adds a user message and runs the subprocess.
func (a *Agent) Send(ctx context.Context, input string) error {
	return a.run(ctx, input, false)
}

// SendMessages adds messages and runs the subprocess.
// Only the text from the last user message is sent as the prompt.
func (a *Agent) SendMessages(
	ctx context.Context,
	msgs ...agent.Message,
) error {
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
func (a *Agent) Continue(ctx context.Context) error {
	a.mu.Lock()
	sid := a.sessionID
	a.mu.Unlock()

	if sid == "" {
		return errors.New("claude: no session to resume")
	}

	return a.run(ctx, "", true)
}

// Subscribe returns a channel of agent events. Each call creates an
// independent subscription. Use [pubsub.After] to replay buffered events.
func (a *Agent) Subscribe(
	ctx context.Context,
	opts ...pubsub.SubscribeOption,
) <-chan pubsub.Event[agent.Event] {
	return a.broker.Subscribe(ctx, opts...)
}

// Wait blocks until the current subprocess completes and returns
// all new messages produced during the run. If the agent is not
// running, Wait returns the result of the last completed run.
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

// Messages returns a copy of the current conversation history.
// Close shuts down the agent's event broker, closing all subscriber
// channels. Subsequent calls to [Agent.Subscribe] return closed channels.
func (a *Agent) Close() {
	a.broker.Shutdown()
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
// and publishes events to the agent's broker as NDJSON lines arrive.
func (a *Agent) run(ctx context.Context, prompt string, resume bool) error {
	a.mu.Lock()

	if a.running {
		a.mu.Unlock()
		return errors.New("claude: already running")
	}

	a.running = true
	a.err = nil
	a.lastMsgs = nil
	a.done = make(chan struct{})

	var inputMsgs []agent.Message
	if prompt != "" && !resume {
		userMsg := agent.NewLLMMessage(ai.UserMessage(prompt))
		a.messages = append(a.messages, userMsg)
		inputMsgs = append(inputMsgs, userMsg)
	}

	sid := a.sessionID
	a.mu.Unlock()

	push := a.broker.Publish

	go func() {
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
		a.lastMsgs = m.messages
		a.err = loopErr
		a.mu.Unlock()

		push(agent.Event{
			Type:     agent.EventAgentEnd,
			Messages: m.messages,
			Usage:    m.usage,
			Err:      loopErr,
		})
	}()

	return nil
}

// stop marks the agent as no longer running and signals [Wait].
func (a *Agent) stop() {
	a.mu.Lock()
	a.running = false
	done := a.done
	a.mu.Unlock()
	if done != nil {
		close(done)
	}
}

// setErr stores an error under the mutex.
func (a *Agent) setErr(err error) {
	a.mu.Lock()
	a.err = err
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
