package cursor

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const simpleTurnJSONL = `{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/repo","session_id":"session-1","model":"GPT-5","permissionMode":"default"}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]},"session_id":"session-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hel"}]},"session_id":"session-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"lo!"}]},"session_id":"session-1"}
{"type":"result","subtype":"success","is_error":false,"duration_ms":123,"duration_api_ms":100,"result":"Hello!","session_id":"session-1"}
`

const secondTurnJSONL = `{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/repo","session_id":"session-1","model":"GPT-5","permissionMode":"default"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Again."}]},"session_id":"session-1"}
{"type":"result","subtype":"success","is_error":false,"duration_ms":50,"duration_api_ms":40,"result":"Again.","session_id":"session-1"}
`

const toolTurnJSONL = `{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/repo","session_id":"session-2","model":"GPT-5","permissionMode":"default"}
{"type":"tool_call","subtype":"started","call_id":"tool-1","tool_call":{"readToolCall":{"args":{"path":"README.md"}}},"session_id":"session-2"}
{"type":"tool_call","subtype":"completed","call_id":"tool-1","tool_call":{"readToolCall":{"args":{"path":"README.md"},"result":{"success":{"content":"# Project\n","isEmpty":false,"totalLines":1,"totalChars":10}}}},"session_id":"session-2"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Read it."}]},"session_id":"session-2"}
{"type":"result","subtype":"success","is_error":false,"duration_ms":80,"duration_api_ms":70,"result":"Read it.","session_id":"session-2"}
`

type runnerCall struct {
	cfg  config
	args runArgs
}

func stubRunner(
	a *Agent,
	outputs ...string,
) (calls func() []runnerCall, restore func()) {
	var (
		mu       sync.Mutex
		captured []runnerCall
		index    int
	)
	orig := a.runFn
	a.runFn = func(_ context.Context, cfg config, args runArgs) (io.ReadCloser, func() error, error) {
		mu.Lock()
		captured = append(captured, runnerCall{cfg: cfg, args: args})
		i := index
		index++
		mu.Unlock()
		if i >= len(outputs) {
			return nil, nil, errors.New("unexpected run")
		}
		return io.NopCloser(strings.NewReader(outputs[i])), func() error { return nil }, nil
	}
	return func() []runnerCall {
		mu.Lock()
		defer mu.Unlock()
		out := make([]runnerCall, len(captured))
		copy(out, captured)
		return out
	}, func() { a.runFn = orig }
}

// Canceling the Run context terminates the Cursor child; the run ends
// with context.Canceled and the agent returns to idle.
func TestAgent_Run_CancelKillsTurn(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5", Name: "gpt-5"})
	reader, writer := io.Pipe()
	started := make(chan struct{})
	a.runFn = func(ctx context.Context, _ config, _ runArgs) (io.ReadCloser, func() error, error) {
		close(started)
		go func() {
			<-ctx.Done()
			_ = writer.CloseWithError(ctx.Err())
		}()
		return reader, func() error { return nil }, nil
	}
	defer a.Close()

	ctx, cancel := context.WithCancel(context.Background())
	s := a.Run(ctx, ai.UserMessage("hi"))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run never started")
	}

	cancel()

	_, err := s.Wait()
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestAgent_Run_SimpleText(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5", Name: "gpt-5"})
	_, restore := stubRunner(a, simpleTurnJSONL)
	defer restore()
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("hi")))
	require.NoError(t, err)

	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		agent.EventAgentEnd,
	}, eventTypes(events))

	assert.Equal(t, "session-1", events[0].SessionID,
		"agent_start carries the session ID",
	)

	last := events[len(events)-1]
	require.Len(t, last.Messages, 1)
	assert.Equal(t, "Hello!", last.Messages[0].Text())
	assert.Equal(t, "session-1", a.SessionID())
}

func TestAgent_Run_ForwardsPromptAndOptions(t *testing.T) {
	a := New(
		ai.Model{ID: "gpt-5", Name: "gpt-5"},
		agent.WithSystemPrompt("Be terse."),
		WithCLIPath("/usr/local/bin/cursor-agent"),
		WithWorkDir("/repo"),
		WithMode("ask"),
		WithSandbox("enabled"),
		WithForce(),
		WithApproveMCPs(),
		WithBrowser(),
	)
	calls, restore := stubRunner(a, simpleTurnJSONL)
	defer restore()
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("ping")).Wait()
	require.NoError(t, err)

	require.Len(t, calls(), 1)
	call := calls()[0]
	assert.Equal(t, "/usr/local/bin/cursor-agent", call.cfg.cliPath)
	assert.Equal(t, "gpt-5", call.cfg.model)
	assert.Equal(t, "/repo", call.cfg.workDir)
	assert.Equal(t, "ask", call.cfg.mode)
	assert.Equal(t, "enabled", call.cfg.sandbox)
	assert.True(t, call.cfg.force)
	assert.True(t, call.cfg.approveMCPs)
	assert.True(t, call.cfg.browser)
	assert.Equal(t, "Be terse.\n\nping", call.args.prompt)
	assert.False(t, call.args.resume)
}

func TestAgent_Run_SecondTurnResumesSession(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5", Name: "gpt-5"})
	calls, restore := stubRunner(a, simpleTurnJSONL, secondTurnJSONL)
	defer restore()
	defer a.Close()

	ctx := context.Background()
	_, err := a.Run(ctx, ai.UserMessage("first")).Wait()
	require.NoError(t, err)

	msgs, err := a.Run(ctx, ai.UserMessage("second")).Wait()
	require.NoError(t, err)

	require.Len(t, msgs, 1)
	assert.Equal(t, "Again.", msgs[0].Text())

	require.Len(t, calls(), 2)
	assert.False(t, calls()[0].args.resume)
	assert.True(t, calls()[1].args.resume)
	assert.Equal(t, "session-1", calls()[1].args.sessionID)
	assert.Equal(t, "session-1", a.SessionID())
}

func TestAgent_Run_ToolCallEvents(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5", Name: "gpt-5"})
	_, restore := stubRunner(a, toolTurnJSONL)
	defer restore()
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("read README")))
	require.NoError(t, err)

	assert.Equal(t, []agent.EventType{
		agent.EventAgentStart,
		agent.EventTurnStart,
		agent.EventToolExecutionStart,
		agent.EventToolExecutionEnd,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		agent.EventAgentEnd,
	}, eventTypes(events))

	var toolEnd *agent.Event
	var turnEnd *agent.Event
	for i := range events {
		switch events[i].Type {
		case agent.EventToolExecutionEnd:
			toolEnd = &events[i]
		case agent.EventTurnEnd:
			turnEnd = &events[i]
		}
	}
	require.NotNil(t, toolEnd)
	assert.Equal(t, "tool-1", toolEnd.ToolCallID)
	assert.Equal(t, "read", toolEnd.ToolName)
	assert.Equal(t, "# Project\n", toolEnd.Result)
	assert.False(t, toolEnd.IsError)

	require.NotNil(t, turnEnd)
	require.Len(t, turnEnd.ToolResults, 1)
	assert.Equal(t, ai.RoleToolResult, turnEnd.ToolResults[0].Role)
	assert.Equal(t, "# Project\n", turnEnd.ToolResults[0].Text())
}

func TestAgent_Run_ConcurrentRejected(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5", Name: "gpt-5"})
	reader, writer := io.Pipe()
	started := make(chan struct{})
	a.runFn = func(_ context.Context, cfg config, args runArgs) (io.ReadCloser, func() error, error) {
		close(started)
		return reader, func() error { return nil }, nil
	}
	defer a.Close()

	ctx := context.Background()
	first := a.Run(ctx, ai.UserMessage("first"))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first run never started")
	}

	_, err := a.Run(ctx, ai.UserMessage("second")).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	_, _ = writer.Write([]byte(simpleTurnJSONL))
	_ = writer.Close()
	_, err = first.Wait()
	require.NoError(t, err)
}

func TestAgent_Run_StartError(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5", Name: "gpt-5"})
	a.runFn = func(_ context.Context, cfg config, args runArgs) (io.ReadCloser, func() error, error) {
		return nil, nil, errors.New("cli not found")
	}
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("hi")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli not found")

	for _, evt := range events {
		assert.NotEqual(t, agent.EventAgentStart, evt.Type,
			"agent_start must not fire when the CLI fails to start",
		)
	}
}

func TestAgent_Run_NoMessages_ReturnsError(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5", Name: "gpt-5"})
	_, err := a.Run(context.Background()).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestFactory_ComposesAgentAndCursorOptions(t *testing.T) {
	agent.RegisterAgent("cursor", New)
	t.Cleanup(func() { agent.UnregisterAgent("cursor") })

	f, ok := agent.GetAgent("cursor")
	require.True(t, ok)

	a := f(
		ai.Model{ID: "gpt-5", Name: "gpt-5"},
		agent.WithMaxTurns(3),
		WithCLIPath("/bin/cursor-agent"),
		WithSessionID("session-xyz"),
		WithMode("plan"),
		WithSandbox("enabled"),
	)

	ca, ok := a.(*Agent)
	require.True(t, ok)
	assert.Equal(t, "gpt-5", ca.cfg.model)
	assert.Equal(t, 3, ca.cfg.maxTurns)
	assert.Equal(t, "/bin/cursor-agent", ca.cfg.cliPath)
	assert.Equal(t, "session-xyz", ca.sessionID)
	assert.Equal(t, "plan", ca.cfg.mode)
	assert.Equal(t, "enabled", ca.cfg.sandbox)
}

func TestBuildArgs_Resume(t *testing.T) {
	got := buildArgs(
		config{
			cliPath: "cursor-agent",
			model:   "gpt-5",
			workDir: "/repo",
			mode:    "ask",
			sandbox: "enabled",
		},
		runArgs{
			prompt:    "next",
			resume:    true,
			sessionID: "session-1",
		},
	)

	assert.Equal(t, []string{
		"-p",
		"--output-format", "stream-json",
		"--model", "gpt-5",
		"--mode", "ask",
		"--sandbox", "enabled",
		"--workspace", "/repo",
		"--resume", "session-1",
		"next",
	}, got)
}

func eventTypes(events []agent.Event) []agent.EventType {
	types := make([]agent.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

// collectRun drains a run's stream with a watchdog timeout, returning
// its events and terminal error.
func collectRun(t *testing.T, s *agent.Stream) ([]agent.Event, error) {
	t.Helper()

	type outcome struct {
		events []agent.Event
		err    error
	}
	done := make(chan outcome, 1)

	go func() {
		var events []agent.Event
		for e, err := range s.Events() {
			if err != nil {
				done <- outcome{events, err}
				return
			}
			events = append(events, e)
		}
		done <- outcome{events, nil}
	}()

	select {
	case o := <-done:
		return o.events, o.err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the run to finish")
		return nil, nil
	}
}
