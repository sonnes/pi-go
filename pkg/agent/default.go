package agent

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Default is the standard [Agent] implementation that manages an
// agentic conversation loop.
type Default struct {
	config config
	state  atomic.Pointer[State]
	mu     sync.Mutex
}

var _ Agent = (*Default)(nil)

// New creates a new [Default] agent with the given model and options.
func New(model ai.Model, opts ...Option) *Default {
	c := config{model: model}
	for _, opt := range opts {
		opt(&c)
	}

	a := &Default{config: c}
	initial := &State{
		messages: make([]Message, len(c.history)),
	}
	copy(initial.messages, c.history)
	a.state.Store(initial)
	return a
}

// Send adds a user message and runs the agent loop.
func (a *Default) Send(ctx context.Context, input string) *EventStream {
	panic("not implemented")
}

// SendMessages adds messages and runs the agent loop.
func (a *Default) SendMessages(ctx context.Context, msgs ...Message) *EventStream {
	panic("not implemented")
}

// Continue resumes from current message state without adding new messages.
func (a *Default) Continue(ctx context.Context) *EventStream {
	panic("not implemented")
}

// State returns a point-in-time snapshot of the agent's runtime state.
func (a *Default) State() State {
	return *a.state.Load()
}
