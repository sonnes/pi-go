package durable_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/session"
)

// recordingAgent is a fake [agent.Agent] that records the messages of
// every Run and replies with one canned assistant message. It is the
// only way to see the boundary durable draws between history and the
// input of a turn: the [agent.Default] loop appends its Run messages to
// its history, so a provider double cannot tell the two apart.
type recordingAgent struct {
	reply string
	// replyContent overrides the canned text reply. A reply that holds a
	// tool call and never gets results leaves the transcript dangling,
	// which is the crash footprint the repair exists for.
	replyContent []ai.Content

	mu      sync.Mutex
	runs    [][]ai.Message
	history []ai.Message
}

func (r *recordingAgent) Run(_ context.Context, msgs ...ai.Message) *agent.Stream {
	r.mu.Lock()
	r.runs = append(r.runs, msgs)
	r.mu.Unlock()

	reply := ai.Message{
		Role:       ai.RoleAssistant,
		Content:    []ai.Content{ai.Text{Text: r.reply}},
		StopReason: ai.StopReasonStop,
	}
	if r.replyContent != nil {
		reply.Content = r.replyContent
		reply.StopReason = ai.StopReasonToolUse
	}
	return agent.NewStream(func(push func(agent.Event)) ([]ai.Message, error) {
		push(agent.Event{Type: agent.EventAgentStart})
		push(agent.Event{Type: agent.EventMessageEnd, Message: &reply})
		push(agent.Event{Type: agent.EventAgentEnd})
		return []ai.Message{reply}, nil
	})
}

func (r *recordingAgent) Messages() []ai.Message { return nil }
func (r *recordingAgent) Close() error           { return nil }

// factory returns the [durable.Factory] that hands back this agent and
// records the history of the most recent build.
func (r *recordingAgent) factory() durable.Factory {
	return func(opts ...agent.Option) (agent.Agent, error) {
		cfg := agent.ApplyOptions(opts...)
		r.mu.Lock()
		r.history = cfg.History
		r.mu.Unlock()
		return r, nil
	}
}

func (r *recordingAgent) run(i int) []ai.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[i]
}

func (r *recordingAgent) runCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

func (r *recordingAgent) builtWith() []ai.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.history
}

func texts(msgs []ai.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Text()
	}
	return out
}

// TestInput_FlowsAsMessagesNotHistory is the contract: the entries of a
// turn reach the loop as the messages of that run. History carries what
// came before them and nothing else.
func TestInput_FlowsAsMessagesNotHistory(t *testing.T) {
	fake := &recordingAgent{reply: "first"}

	da, err := durable.New(
		t.Context(),
		fake.factory(),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer da.Close()

	_, err = da.Run(t.Context(), durable.Text("one")).Wait()
	require.NoError(t, err)

	// A fresh session has nothing before this turn.
	assert.Empty(t, fake.builtWith())

	require.Equal(t, 1, fake.runCount())
	assert.Equal(t, []string{"one"}, texts(fake.run(0)))
}

// TestInput_HistoryHoldsEarlierTurnsOnly checks the split across turns:
// the second run is built with the first turn, and carries only its own
// input as messages.
func TestInput_HistoryHoldsEarlierTurnsOnly(t *testing.T) {
	fake := &recordingAgent{reply: "reply"}

	da, err := durable.New(
		t.Context(),
		fake.factory(),
		durable.WithStore(session.NewMemoryStore()),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer da.Close()

	_, err = da.Run(t.Context(), durable.Text("one")).Wait()
	require.NoError(t, err)
	_, err = da.Run(t.Context(), durable.Text("two")).Wait()
	require.NoError(t, err)

	// The second build sees the first turn, and not the input of this one.
	assert.Equal(t, []string{"one", "reply"}, texts(fake.builtWith()))

	require.Equal(t, 2, fake.runCount())
	assert.Equal(t, []string{"two"}, texts(fake.run(1)))
}

// TestInput_ResumeRehydratesHistoryOnly checks that a reopened session
// puts the stored transcript in history, not in the messages of the run.
func TestInput_ResumeRehydratesHistoryOnly(t *testing.T) {
	store := session.NewMemoryStore()

	first := &recordingAgent{reply: "stored"}
	da, err := durable.New(
		t.Context(),
		first.factory(),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	_, err = da.Run(t.Context(), durable.Text("one")).Wait()
	require.NoError(t, err)
	require.NoError(t, da.Close())

	second := &recordingAgent{reply: "resumed"}
	reopened, err := durable.New(
		t.Context(),
		second.factory(),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer reopened.Close()

	_, err = reopened.Run(t.Context(), durable.Text("two")).Wait()
	require.NoError(t, err)

	assert.Equal(t, []string{"one", "stored"}, texts(second.builtWith()))
	assert.Equal(t, []string{"two"}, texts(second.run(0)))
}

// TestInput_KeepsEveryEntryOfTheTurnInOrder checks that a turn with
// several entries hands all of them to the run, in the order the caller
// wrote them. An ephemeral entry keeps its position among them.
func TestInput_KeepsEveryEntryOfTheTurnInOrder(t *testing.T) {
	fake := &recordingAgent{reply: "ok"}

	da, err := durable.New(
		t.Context(),
		fake.factory(),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer da.Close()

	_, err = da.Run(
		t.Context(),
		durable.Ephemeral(durable.Text("context")),
		durable.Text("question"),
	).Wait()
	require.NoError(t, err)

	assert.Equal(t, []string{"context", "question"}, texts(fake.run(0)))
	assert.Empty(t, fake.builtWith())
}

// TestInput_RepairsDanglingToolCallInHistory checks that the repair of
// an interrupted tool call still happens. It belongs to history: a
// dangling call is the footprint of a run that crashed, never of the
// input the caller just wrote.
func TestInput_RepairsDanglingToolCallInHistory(t *testing.T) {
	store := session.NewMemoryStore()
	call := ai.ToolCall{ID: "call-1", Name: "search", Arguments: map[string]any{}}

	// The first run ends on a tool call whose results never arrive.
	crashed := &recordingAgent{replyContent: []ai.Content{call}}
	da, err := durable.New(
		t.Context(),
		crashed.factory(),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	_, err = da.Run(t.Context(), durable.Text("find it")).Wait()
	require.NoError(t, err)
	require.NoError(t, da.Close())

	fake := &recordingAgent{reply: "recovered"}
	reopened, err := durable.New(
		t.Context(),
		fake.factory(),
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer reopened.Close()

	_, err = reopened.Run(t.Context(), durable.Text("carry on")).Wait()
	require.NoError(t, err)

	// History gains the synthesized tool result; the run still carries
	// only the input of this turn.
	built := fake.builtWith()
	require.Len(t, built, 3)
	assert.Equal(t, ai.RoleToolResult, built[2].Role)
	assert.Equal(t, []string{"carry on"}, texts(fake.run(0)))
}
