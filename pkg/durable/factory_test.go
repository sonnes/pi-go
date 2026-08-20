package durable_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/session"
)

// recordingFactory delegates to the default loop over lm and records the
// options of every build. It is the seam a CLI-backed agent uses in
// production, exercised here with an ordinary loop.
type recordingFactory struct {
	lm ai.LanguageModel

	mu      sync.Mutex
	configs []agent.Config
}

func (f *recordingFactory) Factory() durable.Factory {
	return func(opts ...agent.Option) (agent.Agent, error) {
		f.mu.Lock()
		f.configs = append(f.configs, agent.ApplyOptions(opts...))
		f.mu.Unlock()
		return agent.New(f.lm, opts...), nil
	}
}

func (f *recordingFactory) calls() []agent.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]agent.Config, len(f.configs))
	copy(out, f.configs)
	return out
}

// TestFactory_BuildsInnerPerRun checks that durable calls the factory
// once for each run and hands it the rehydrated history of that run.
func TestFactory_BuildsInnerPerRun(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("first"),
		textStream("second"),
	}}
	rec := &recordingFactory{lm: testModel(prov)}

	da, err := durable.New(
		t.Context(),
		rec.Factory(),
		durable.WithStore(session.NewMemoryStore()),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer da.Close()

	run(t, da, "one")
	run(t, da, "two")

	calls := rec.calls()
	require.Len(t, calls, 2)

	// A fresh session has nothing before its first turn.
	assert.Empty(t, calls[0].History)

	// The second build rehydrates the first turn. The input of this turn
	// is not in it — that reaches the loop as the messages of the run.
	// See TestInput_HistoryHoldsEarlierTurnsOnly.
	require.Len(t, calls[1].History, 2)
	assert.Equal(t, "one", calls[1].History[0].Text())
	assert.Equal(t, "first", calls[1].History[1].Text())
}

// TestFactory_ForwardsBaseOptions checks that the options given to
// [durable.New] reach the factory alongside the history of the run.
func TestFactory_ForwardsBaseOptions(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	rec := &recordingFactory{lm: testModel(prov)}

	da, err := durable.New(
		t.Context(),
		rec.Factory(),
		durable.WithSessionID("s1"),
		agent.WithSystemPrompt("be brief"),
		agent.WithMaxTurns(3),
	)
	require.NoError(t, err)
	defer da.Close()

	run(t, da, "hi")

	calls := rec.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "be brief", calls[0].SystemPrompt)
	assert.Equal(t, 3, calls[0].MaxTurns)
}

// TestFactory_ErrorFailsRunAndPersistsNothing checks that a factory
// error reaches the caller on the stream, and that the run leaves no
// trace. A configuration that cannot start must not litter the
// transcript with an input entry that never got an answer.
func TestFactory_ErrorFailsRunAndPersistsNothing(t *testing.T) {
	store := session.NewMemoryStore()
	sentinel := errors.New("cli not found")

	da, err := durable.New(
		t.Context(),
		func(...agent.Option) (agent.Agent, error) { return nil, sentinel },
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer da.Close()

	_, err = da.Run(t.Context(), durable.Text("hi")).Wait()
	require.ErrorIs(t, err, sentinel)

	entries, err := store.LoadEntries(t.Context(), "s1")
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.Empty(t, da.LeafID())
}

// TestFactory_NilRunsNothingButAppends checks the transcript-only agent:
// a caller that repairs history needs a session, not a loop. New accepts
// a nil factory, Append works, and only Run reports the missing loop.
func TestFactory_NilRunsNothingButAppends(t *testing.T) {
	store := session.NewMemoryStore()

	da, err := durable.New(
		t.Context(),
		nil,
		durable.WithStore(store),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer da.Close()

	repair := session.NewMessageEntry(ai.ErrorToolResultMessage("call-1", "ask", "interrupted"))
	require.NoError(t, da.Append(t.Context(), repair))

	entries, err := store.LoadEntries(t.Context(), "s1")
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	_, err = da.Run(t.Context(), durable.Text("hi")).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory")
}

// TestFactory_ForkInherits checks that a child session builds its inner
// loop from the same factory as its parent.
func TestFactory_ForkInherits(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("parent"),
		textStream("child"),
	}}
	rec := &recordingFactory{lm: testModel(prov)}

	parent, err := durable.New(
		t.Context(),
		rec.Factory(),
		durable.WithStore(session.NewMemoryStore()),
		durable.WithSessionID("parent"),
	)
	require.NoError(t, err)
	defer parent.Close()

	run(t, parent, "one")

	child, err := parent.Fork(t.Context(), "child")
	require.NoError(t, err)
	defer child.Close()

	run(t, child, "two")

	assert.Len(t, rec.calls(), 2)
}

// TestModel_BuildsDefaultLoop checks that [durable.Model] is the factory
// for an ordinary API-backed session.
func TestModel_BuildsDefaultLoop(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{textStream("hello")}}

	da, err := durable.New(
		t.Context(),
		durable.Model(testModel(prov)),
		durable.WithSessionID("s1"),
	)
	require.NoError(t, err)
	defer da.Close()

	msgs, err := da.Run(t.Context(), durable.Text("hi")).Wait()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "hello", msgs[0].Text())
	assert.Equal(t, 1, prov.promptCount())
}
