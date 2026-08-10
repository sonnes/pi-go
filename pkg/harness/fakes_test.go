package harness

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
	"github.com/sonnes/pi-go/pkg/session"
)

// newTestHarness builds a harness over the mock catalog with the given
// extra options, failing the test if construction errors.
func newTestHarness(t *testing.T, p *mockProvider, opts ...agent.Option) *Harness {
	t.Helper()
	base := []agent.Option{
		WithCatalog(testCatalog(p)),
		WithDefaultModel("mock/small"),
	}
	h, err := New(append(base, opts...)...)
	require.NoError(t, err)
	return h
}

// --- provider / catalog doubles ---

// mockProvider replays scripted [ai.EventStream] responses in order and
// records every prompt it is handed.
type mockProvider struct {
	mu        sync.Mutex
	prompts   []ai.Prompt
	responses []*ai.EventStream
	callIdx   int
}

func mockModels() []ai.Model {
	return []ai.Model{
		{ID: "small", Name: "Mock Small", ToolCall: true},
		{ID: "large", Name: "Mock Large", ToolCall: true},
	}
}

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
		return ai.NewEventStream(func(func(ai.Event)) (*ai.Message, error) {
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

// testCatalog registers the mock provider under "mock" so specs like
// "mock/small" resolve.
func testCatalog(p *mockProvider) *catalog.Catalog {
	c := catalog.New()
	c.RegisterTextProvider("mock", p, mockModels()...)
	return c
}

// --- resolver doubles ---

// fakeResolver satisfies all three resolver interfaces.
type fakeResolver struct {
	name   string
	agents []def.Agent
	skills []def.Skill
	docs   []def.Instructions
	err    error

	calls int
}

func (f *fakeResolver) Agents(context.Context) ([]def.Agent, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.agents, nil
}

func (f *fakeResolver) Skills(context.Context) ([]def.Skill, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.skills, nil
}

func (f *fakeResolver) Instructions(context.Context) ([]def.Instructions, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.docs, nil
}

// mutableResolver is an agent resolver whose list can change between
// builds, standing in for a file edited mid-session.
type mutableResolver struct {
	agents []def.Agent
}

func (m *mutableResolver) set(agents ...def.Agent) { m.agents = agents }

func (m *mutableResolver) Agents(context.Context) ([]def.Agent, error) {
	return m.agents, nil
}

// flakyStore fails AppendEntries while fail is set, standing in for a
// store that was briefly unavailable on a session's first run.
type flakyStore struct {
	session.Store
	fail bool
}

func (s *flakyStore) AppendEntries(
	ctx context.Context,
	sessionID string,
	entries ...session.Entry,
) error {
	if s.fail {
		return fmt.Errorf("flaky store: unavailable")
	}
	return s.Store.AppendEntries(ctx, sessionID, entries...)
}

// noopTool is a minimal named tool for toolbox and filter tests.
func noopTool(name string) ai.Tool {
	return descTool(name, "test tool "+name)
}

// descTool is a noop tool with a chosen description, so an override can
// be told apart from the tool it replaced.
func descTool(name, desc string) ai.Tool {
	return ai.DefineTool(
		name,
		desc,
		func(context.Context, struct{}) (string, error) { return name, nil },
	)
}

// --- builders and seeders ---

// capturingBuilder records the Env it is handed and returns a fixed
// prompt, so a test can assert on the snapshot the harness assembled
// without depending on how prompt.Default renders it.
func capturingBuilder(into **prompt.Env) prompt.Builder {
	return func(_ context.Context, env *prompt.Env) (string, error) {
		*into = env
		return "SYSTEM", nil
	}
}

// seedText returns a Seeder that emits one text entry.
func seedText(text string) prompt.Seeder {
	return func(context.Context, *prompt.Env) ([]session.Entry, error) {
		return []session.Entry{durable.Text(text)}, nil
	}
}

// --- assertions ---

// findTool returns the named tool from a list, or nil.
func findTool(tools []ai.Tool, name string) ai.Tool {
	for _, t := range tools {
		if t.Info().Name == name {
			return t
		}
	}
	return nil
}

func toolNames(infos []ai.ToolInfo) []string {
	out := make([]string, len(infos))
	for i, info := range infos {
		out[i] = info.Name
	}
	return out
}

func agentNames(agents []def.Agent) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.Name
	}
	return out
}

func skillNames(skills []def.Skill) []string {
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = s.Name
	}
	return out
}

// promptText joins every message in a prompt, so a test can ask what
// the model saw without caring where in the history it landed.
func promptText(p ai.Prompt) string {
	var b strings.Builder
	for _, m := range p.Messages {
		b.WriteString(m.Text())
		b.WriteString("\n")
	}
	return b.String()
}

func countText(entries []session.Entry, want string) int {
	n := 0
	for _, e := range entries {
		if me, ok := session.AsMessageEntry(e); ok && strings.Contains(me.Text(), want) {
			n++
		}
	}
	return n
}
