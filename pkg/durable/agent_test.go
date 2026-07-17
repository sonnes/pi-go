package durable_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/sonnes/pi-go/pkg/session/fs"
)

// --- test doubles ---

type testState struct {
	Title string
}

// mockProvider returns scripted [ai.EventStream] responses in order and
// records every prompt it receives.
type mockProvider struct {
	mu        sync.Mutex
	prompts   []ai.Prompt
	responses []*ai.EventStream
	callIdx   int
}

func (m *mockProvider) Provider() string { return "mock" }

func (m *mockProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	p ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.prompts = append(m.prompts, p)

	if m.callIdx >= len(m.responses) {
		return ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
			return nil, fmt.Errorf("mock: no more responses (call %d)", m.callIdx)
		})
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp
}

func (m *mockProvider) prompt(i int) ai.Prompt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prompts[i]
}

func (m *mockProvider) promptCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.prompts)
}

func textStream(text string) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		push(ai.Event{Type: ai.EventTextStart})
		push(ai.Event{Type: ai.EventTextDelta, Delta: text})
		push(ai.Event{Type: ai.EventTextEnd, Content: text})
		return &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    []ai.Content{ai.Text{Text: text}},
			StopReason: ai.StopReasonStop,
		}, nil
	})
}

func toolCallStream(call ai.ToolCall) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		push(ai.Event{Type: ai.EventToolStart})
		push(ai.Event{Type: ai.EventToolEnd, ToolCall: &call})
		return &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    []ai.Content{call},
			StopReason: ai.StopReasonToolUse,
		}, nil
	})
}

func errorStream(err error) *ai.EventStream {
	return ai.NewEventStream(func(_ func(ai.Event)) (*ai.Message, error) {
		return nil, err
	})
}

// testLM binds a text provider to a fixed test model.
func testLM(p ai.TextProvider) ai.LanguageModel {
	return ai.NewLanguageModel(ai.Model{ID: "test-model"}, p)
}

// collect drains a durable stream, returning its events, result, and
// terminal error.
func collect(t *testing.T, s *durable.Stream) ([]durable.Event, []ai.Message, error) {
	t.Helper()
	var events []durable.Event
	for e, err := range s.Events() {
		if err != nil {
			return events, nil, err
		}
		events = append(events, e)
	}
	msgs, err := s.Wait()
	return events, msgs, err
}

// ofType filters durable events by type.
func ofType(events []durable.Event, et durable.EventType) []durable.Event {
	var out []durable.Event
	for _, e := range events {
		if e.Type == et {
			out = append(out, e)
		}
	}
	return out
}

// recordingPublisher captures session events as they are published.
type recordingPublisher struct {
	mu     sync.Mutex
	events []durable.Event
}

func (p *recordingPublisher) Publish(e durable.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}

func (p *recordingPublisher) all() []durable.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]durable.Event(nil), p.events...)
}

// liftedOf filters lifted agent events by inner type.
func liftedOf(events []durable.Event, at agent.EventType) []durable.Event {
	var out []durable.Event
	for _, e := range events {
		if e.Type == durable.EventAgent && e.Agent.Type == at {
			out = append(out, e)
		}
	}
	return out
}

func openTestAgent(
	t *testing.T,
	store session.Store[testState],
	id string,
	prov ai.TextProvider,
	extra ...agent.Option,
) *durable.Agent[testState] {
	t.Helper()
	opts := append(
		[]agent.Option{durable.WithStore(store), durable.WithSessionID(id)},
		extra...,
	)
	da, err := durable.New[testState](t.Context(), testLM(prov), opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = da.Close() })
	return da
}

// openWithPublisher opens a test agent with a recording publisher.
func openWithPublisher(
	t *testing.T,
	store session.Store[testState],
	id string,
	prov ai.TextProvider,
) (*durable.Agent[testState], *recordingPublisher) {
	t.Helper()
	pub := &recordingPublisher{}
	da, err := durable.New[testState](
		t.Context(),
		testLM(prov),
		durable.WithStore(store),
		durable.WithSessionID(id),
		durable.WithPublisher(pub),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = da.Close() })
	return da, pub
}

// run drives one turn to completion, failing the test on error.
func run(t *testing.T, da *durable.Agent[testState], text string) {
	t.Helper()
	_, err := da.Run(t.Context(), ai.UserMessage(text)).Wait()
	require.NoError(t, err)
}

// --- Open / Run / persistence ---

func TestRun_EventsAndPersistence(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{textStream("hello")}}
	da := openTestAgent(t, store, "s1", prov)

	events, msgs, err := collect(t, da.Run(t.Context(), ai.UserMessage("hi")))
	require.NoError(t, err)

	// Wait returns the run's new messages.
	require.Len(t, msgs, 1)
	assert.Equal(t, "hello", msgs[0].Text())

	// The stream is turn-scoped: lifted agent events only, starting
	// with agent_start.
	require.NotEmpty(t, events)
	assert.Equal(t, durable.EventAgent, events[0].Type)
	assert.Equal(t, agent.EventAgentStart, events[0].Agent.Type)

	// agent_start carries the input persistence receipt.
	starts := liftedOf(events, agent.EventAgentStart)
	require.Len(t, starts, 1)
	require.Len(t, starts[0].Entries, 1)
	assert.NotEmpty(t, starts[0].LeafID)

	// message_end carries the assistant persistence receipt.
	ends := liftedOf(events, agent.EventMessageEnd)
	require.Len(t, ends, 1)
	require.Len(t, ends[0].Entries, 1)
	assert.Equal(t, ends[0].LeafID, da.LeafID())

	// agent_end arrives lifted.
	assert.Len(t, liftedOf(events, agent.EventAgentEnd), 1)

	// Store holds user + assistant entries, chained parent → child.
	_, entries, err := store.LoadSession(t.Context(), "s1")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	user, ok := session.AsMessageEntry(entries[0])
	require.True(t, ok)
	assert.Equal(t, ai.RoleUser, user.Message.Role)
	assert.Empty(t, user.Header().ParentID)
	asst, ok := session.AsMessageEntry(entries[1])
	require.True(t, ok)
	assert.Equal(t, ai.RoleAssistant, asst.Message.Role)
	assert.Equal(t, user.Header().ID, asst.Header().ParentID)
	assert.Equal(t, asst.Header().ID, da.LeafID())
}

func TestRun_ToolLoopPersistsEveryMessage(t *testing.T) {
	echo := ai.DefineTool(
		"echo",
		"Echo the input text.",
		func(_ context.Context, in struct {
			Text string `json:"text"`
		}) (string, error) {
			return in.Text, nil
		},
	)
	call := ai.ToolCall{
		ID:        "call-1",
		Name:      "echo",
		Arguments: map[string]any{"text": "ping"},
	}
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{
		toolCallStream(call),
		textStream("done"),
	}}
	da := openTestAgent(t, store, "s1", prov, agent.WithTools(echo))

	events, _, err := collect(t, da.Run(t.Context(), ai.UserMessage("go")))
	require.NoError(t, err)

	// Three message_end receipts: assistant(tool_use), tool result,
	// final assistant — each carrying exactly one persisted entry.
	ends := liftedOf(events, agent.EventMessageEnd)
	require.Len(t, ends, 3)
	for _, e := range ends {
		assert.Len(t, e.Entries, 1)
	}

	// Store: user, assistant(tool_use), tool_result, assistant.
	_, entries, err := store.LoadSession(t.Context(), "s1")
	require.NoError(t, err)
	require.Len(t, entries, 4)
	roles := make([]ai.Role, 0, 4)
	for i, e := range entries {
		me, ok := session.AsMessageEntry(e)
		require.True(t, ok)
		roles = append(roles, me.Message.Role)
		if i > 0 {
			assert.Equal(t, entries[i-1].Header().ID, e.Header().ParentID)
		}
	}
	assert.Equal(t, []ai.Role{
		ai.RoleUser,
		ai.RoleAssistant,
		ai.RoleToolResult,
		ai.RoleAssistant,
	}, roles)
}

func TestRun_ProviderErrorKeepsInputOnly(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{errorStream(assert.AnError)}}
	da := openTestAgent(t, store, "s1", prov)

	_, _, err := collect(t, da.Run(t.Context(), ai.UserMessage("hi")))
	assert.ErrorIs(t, err, assert.AnError)

	// Input persisted before the run; nothing else landed.
	_, entries, lerr := store.LoadSession(t.Context(), "s1")
	require.NoError(t, lerr)
	require.Len(t, entries, 1)
}

func TestNew_ResumeHydratesHistory(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("nice to meet you, Ravi"),
		textStream("your name is Ravi"),
	}}

	da, err := durable.New[testState](t.Context(), testLM(prov), durable.WithStore(store), durable.WithSessionID("u42"))
	require.NoError(t, err)
	run(t, da, "My name is Ravi.")
	leafBefore := da.LeafID()
	require.NoError(t, da.Close())

	// Process B: same ID resumes the same conversation; session_init
	// is published at Open with the resume leaf.
	pub := &recordingPublisher{}
	da, err = durable.New[testState](t.Context(), testLM(prov), durable.WithStore(store), durable.WithSessionID("u42"), durable.WithPublisher(pub))
	require.NoError(t, err)
	defer da.Close()

	inits := ofType(pub.all(), durable.EventSessionInit)
	require.Len(t, inits, 1)
	assert.Equal(t, "u42", inits[0].SessionID)
	assert.Equal(t, leafBefore, inits[0].LeafID)

	events, _, err := collect(t, da.Run(t.Context(), ai.UserMessage("What's my name?")))
	require.NoError(t, err)

	// The run stream carries no session events.
	assert.Empty(t, ofType(events, durable.EventSessionInit))

	// The model saw the full prior conversation plus the new question.
	p := prov.prompt(1)
	require.Len(t, p.Messages, 3)
	assert.Equal(t, "My name is Ravi.", p.Messages[0].Text())
	assert.Equal(t, "nice to meet you, Ravi", p.Messages[1].Text())
	assert.Equal(t, "What's my name?", p.Messages[2].Text())
}

func TestNew_Defaults(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}

	// No store, no session ID: in-memory store, generated ID.
	da, err := durable.New[testState](t.Context(), testLM(prov))
	require.NoError(t, err)
	defer da.Close()

	assert.NotEmpty(t, da.Session().ID)

	run(t, da, "hi")
	entries, err := da.Entries(t.Context())
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestNew_StoreTypeMismatch(t *testing.T) {
	other := session.NewMemoryStore[int]()
	_, err := durable.New[testState](
		t.Context(),
		testLM(&mockProvider{}),
		durable.WithStore(other),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store")
}

func TestNew_TwoInstancesGrowSiblingBranches(t *testing.T) {
	store := session.NewMemoryStore[testState]()

	// Seed one turn so both instances share a common leaf.
	prov := &mockProvider{responses: []*ai.EventStream{textStream("root answer")}}
	seed, err := durable.New[testState](t.Context(), testLM(prov), durable.WithStore(store), durable.WithSessionID("shared"))
	require.NoError(t, err)
	run(t, seed, "root question")
	require.NoError(t, seed.Close())

	// Two independent instances of the same session.
	provA := &mockProvider{responses: []*ai.EventStream{textStream("answer a")}}
	a, err := durable.New[testState](t.Context(), testLM(provA), durable.WithStore(store), durable.WithSessionID("shared"))
	require.NoError(t, err)
	defer a.Close()

	provB := &mockProvider{responses: []*ai.EventStream{textStream("answer b")}}
	b, err := durable.New[testState](t.Context(), testLM(provB), durable.WithStore(store), durable.WithSessionID("shared"))
	require.NoError(t, err)
	defer b.Close()

	run(t, a, "question a")
	run(t, b, "question b")

	// Both appended from the same resume leaf: the log holds two
	// sibling branches, nothing lost, nothing overwritten.
	_, entries, err := store.LoadSession(t.Context(), "shared")
	require.NoError(t, err)
	require.Len(t, entries, 6)

	sharedLeaf := entries[1].Header().ID // root answer
	children := 0
	for _, e := range entries {
		if e.Header().ParentID == sharedLeaf {
			children++
		}
	}
	assert.Equal(t, 2, children, "each instance grew its own branch")
}

// --- repair ---

func TestMessages_RepairsDanglingToolCalls(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{textStream("recovered")}}
	da := openTestAgent(t, store, "s1", prov)

	// Simulate a crash after the assistant message persisted but before
	// its tool results did.
	dangling := ai.Message{
		Role: ai.RoleAssistant,
		Content: []ai.Content{ai.ToolCall{
			ID:        "call-9",
			Name:      "echo",
			Arguments: map[string]any{"text": "lost"},
		}},
		StopReason: ai.StopReasonToolUse,
	}
	require.NoError(t, da.Append(t.Context(),
		session.NewMessageEntry(ai.UserMessage("go")),
		session.NewMessageEntry(dangling),
	))

	// The model view synthesizes an interrupted tool result.
	msgs := da.Messages()
	require.Len(t, msgs, 3)
	assert.Equal(t, ai.RoleToolResult, msgs[2].Role)
	assert.Equal(t, "call-9", msgs[2].ToolCallID)
	assert.True(t, msgs[2].IsError)

	// The next run's provider call sees the repaired history.
	_, _, err := collect(t, da.Run(t.Context(), ai.UserMessage("continue")))
	require.NoError(t, err)
	p := prov.prompt(0)
	require.Len(t, p.Messages, 4)
	assert.Equal(t, ai.RoleToolResult, p.Messages[2].Role)
}

// --- session verbs ---

func TestSetState(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	da, pub := openWithPublisher(t, store, "s1", prov)

	require.NoError(t, da.SetState(t.Context(), testState{Title: "Refund"}))
	assert.Equal(t, "Refund", da.Session().State.Title)

	// State folded into the store record.
	sess, _, err := store.LoadSession(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "Refund", sess.State.Title)

	// session_updated was published at the SetState call, carrying the
	// appended StateEntry — no run needed.
	published := pub.all()
	updated := ofType(published, durable.EventSessionUpdated)
	require.Len(t, updated, 1)
	require.Len(t, updated[0].Entries, 1)
	_, isState := updated[0].Entries[0].(session.StateEntry[testState])
	assert.True(t, isState)
	assert.Equal(t, da.LeafID(), updated[0].LeafID)

	// session_init preceded it.
	require.NotEmpty(t, published)
	assert.Equal(t, durable.EventSessionInit, published[0].Type)

	// The run stream carries no session events.
	events, _, err := collect(t, da.Run(t.Context(), ai.UserMessage("hi")))
	require.NoError(t, err)
	assert.Empty(t, ofType(events, durable.EventSessionUpdated))
}

func TestBranch(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("first answer"),
		textStream("second answer"),
		textStream("third answer"),
	}}
	da, pub := openWithPublisher(t, store, "s1", prov)

	run(t, da, "first question")
	checkpoint := da.LeafID()

	run(t, da, "second question")
	fromLeaf := da.LeafID()

	require.NoError(t, da.Branch(t.Context(), checkpoint))

	// session_branched published at the Branch call.
	branched := ofType(pub.all(), durable.EventSessionBranched)
	require.Len(t, branched, 1)
	assert.Equal(t, checkpoint, branched[0].LeafID)
	assert.Equal(t, fromLeaf, branched[0].FromID)

	_, _, err := collect(t, da.Run(t.Context(), ai.UserMessage("third question")))
	require.NoError(t, err)

	// Model view: first exchange + third exchange; second is abandoned.
	msgs := da.Messages()
	require.Len(t, msgs, 4)
	assert.Equal(t, "first question", msgs[0].Text())
	assert.Equal(t, "first answer", msgs[1].Text())
	assert.Equal(t, "third question", msgs[2].Text())
	assert.Equal(t, "third answer", msgs[3].Text())

	// The abandoned branch is still in the log.
	entries, err := da.Entries(t.Context())
	require.NoError(t, err)
	assert.Len(t, entries, 6)
}

func TestBranch_UnknownEntry(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	da := openTestAgent(t, store, "s1", &mockProvider{})

	assert.Error(t, da.Branch(t.Context(), "nope"))
}

func TestFork(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("answer"),
		textStream("alt answer"),
		textStream("src answer"),
	}}
	da, pub := openWithPublisher(t, store, "src", prov)

	run(t, da, "question")

	alt, err := da.Fork(t.Context(), "alt")
	require.NoError(t, err)
	defer alt.Close()

	assert.Equal(t, "src", alt.Session().ParentID)

	// The fork publishes on the source's publisher: session_forked for
	// the source, session_init for the inherited child.
	published := pub.all()
	forked := ofType(published, durable.EventSessionForked)
	require.Len(t, forked, 1)
	assert.Equal(t, "alt", forked[0].SessionID)
	assert.Equal(t, "src", forked[0].ParentID)
	inits := ofType(published, durable.EventSessionInit)
	require.Len(t, inits, 2)
	assert.Equal(t, "alt", inits[1].SessionID)
	assert.Equal(t, alt.LeafID(), inits[1].LeafID)

	// The fork carries the active path with fresh IDs.
	srcEntries, err := da.Entries(t.Context())
	require.NoError(t, err)
	altEntries, err := alt.Entries(t.Context())
	require.NoError(t, err)
	require.Len(t, altEntries, len(srcEntries))
	for i := range altEntries {
		assert.NotEqual(t, srcEntries[i].Header().ID, altEntries[i].Header().ID)
	}

	// Fork's history reaches the model; source is untouched by alt runs.
	run(t, alt, "what if instead?")
	p := prov.prompt(1)
	assert.Equal(t, "question", p.Messages[0].Text())

	srcAfter, err := da.Entries(t.Context())
	require.NoError(t, err)
	assert.Len(t, srcAfter, len(srcEntries))

	// Taken ID fails, and a failed fork publishes nothing new.
	_, err = da.Fork(t.Context(), "alt")
	assert.ErrorIs(t, err, session.ErrSessionExists)
	assert.Len(t, ofType(pub.all(), durable.EventSessionForked), 1)

	// The source's run stream carries no session events.
	events, _, err := collect(t, da.Run(t.Context(), ai.UserMessage("back to src")))
	require.NoError(t, err)
	assert.Empty(t, ofType(events, durable.EventSessionForked))
}

// --- custom entries / views ---

type artifactEntry struct {
	session.CustomEntry
	Title string
}

func TestAppend_EntriesAndViews(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	da := openTestAgent(t, store, "s1", prov)

	run(t, da, "hi")

	err := da.Append(t.Context(), artifactEntry{
		CustomEntry: session.CustomEntry{Kind: "artifact"},
		Title:       "draft",
	})
	require.NoError(t, err)

	// Full log includes the custom entry with assigned tree fields.
	entries, err := da.Entries(t.Context())
	require.NoError(t, err)
	arts := session.Filter[artifactEntry](entries)
	require.Len(t, arts, 1)
	assert.Equal(t, "draft", arts[0].Title)
	assert.NotEmpty(t, arts[0].Header().ID)
	assert.Equal(t, arts[0].Header().ID, da.LeafID())

	// Transcript shows it; the model never sees it.
	transcript, err := da.Transcript(t.Context())
	require.NoError(t, err)
	assert.Len(t, transcript, 3)
	assert.Len(t, da.Messages(), 2)
}

// --- compaction ---

func TestCompact(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("answer one"),
		textStream("answer two"),
		textStream("summary of turn one"), // summarizer call
	}}
	da, pub := openWithPublisher(t, store, "s1", prov)

	run(t, da, "question one")
	run(t, da, "question two")

	require.NoError(t, da.Compact(t.Context(), durable.KeepTurns(1)))

	// session_compacted published at the Compact call.
	compacted := ofType(pub.all(), durable.EventSessionCompacted)
	require.Len(t, compacted, 1)
	require.Len(t, compacted[0].Entries, 1)
	_, isComp := compacted[0].Entries[0].(session.CompactionEntry)
	assert.True(t, isComp)

	// The summarizer saw only the compacted prefix plus an instruction.
	p := prov.prompt(2)
	require.GreaterOrEqual(t, len(p.Messages), 3)
	assert.Equal(t, "question one", p.Messages[0].Text())
	assert.Equal(t, "answer one", p.Messages[1].Text())

	// A CompactionEntry landed; nothing was deleted.
	entries, err := da.Entries(t.Context())
	require.NoError(t, err)
	comps := session.Filter[session.CompactionEntry](entries)
	require.Len(t, comps, 1)
	assert.Equal(t, "summary of turn one", comps[0].Summary)
	assert.Len(t, entries, 5)

	// Model view: summary + kept tail.
	msgs := da.Messages()
	require.Len(t, msgs, 3)
	assert.Equal(t, "summary of turn one", msgs[0].Text())
	assert.Equal(t, "question two", msgs[1].Text())
	assert.Equal(t, "answer two", msgs[2].Text())
}

func TestCompact_CustomPrompt(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("answer one"),
		textStream("answer two"),
		textStream("summary"), // summarizer call
	}}
	da := openTestAgent(t, store, "s1", prov)

	run(t, da, "question one")
	run(t, da, "question two")

	require.NoError(t, da.Compact(
		t.Context(),
		durable.KeepTurns(1),
		durable.CompactPrompt("Condense into bullet points."),
	))

	// The summarizer received the custom instruction as its last message.
	p := prov.prompt(2)
	require.NotEmpty(t, p.Messages)
	assert.Equal(t, "Condense into bullet points.", p.Messages[len(p.Messages)-1].Text())
}

func TestCompact_NothingToCompact(t *testing.T) {
	store := session.NewMemoryStore[testState]()
	prov := &mockProvider{responses: []*ai.EventStream{textStream("a")}}
	da := openTestAgent(t, store, "s1", prov)

	run(t, da, "q")

	require.NoError(t, da.Compact(t.Context(), durable.KeepTurns(5)))

	entries, err := da.Entries(t.Context())
	require.NoError(t, err)
	assert.Empty(t, session.Filter[session.CompactionEntry](entries))
}

// --- fs store round-trip ---

func TestNew_ResumeFromFileStore(t *testing.T) {
	store, err := fs.New[testState](t.TempDir())
	require.NoError(t, err)
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("hello Ravi"),
		textStream("Ravi"),
	}}

	da, err := durable.New[testState](t.Context(), testLM(prov), durable.WithStore(store), durable.WithSessionID("u1"))
	require.NoError(t, err)
	run(t, da, "I'm Ravi.")
	require.NoError(t, da.SetState(t.Context(), testState{Title: "Intro"}))
	require.NoError(t, da.Close())

	// Reopen: entries and state round-trip through the JSONL codec.
	da, err = durable.New[testState](t.Context(), testLM(prov), durable.WithStore(store), durable.WithSessionID("u1"))
	require.NoError(t, err)
	defer da.Close()

	assert.Equal(t, "Intro", da.Session().State.Title)

	run(t, da, "My name?")
	p := prov.prompt(1)
	require.Len(t, p.Messages, 3)
	assert.Equal(t, "I'm Ravi.", p.Messages[0].Text())
	assert.Equal(t, "hello Ravi", p.Messages[1].Text())
}
