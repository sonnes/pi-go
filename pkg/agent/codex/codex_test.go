package codex

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

const simpleTurnJSONL = `Reading additional input from stdin...
{"type":"thread.started","thread_id":"thread-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Hello!"}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":3,"output_tokens":5,"reasoning_output_tokens":2}}
`

const secondTurnJSONL = `{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Again."}}
{"type":"turn.completed","usage":{"input_tokens":4,"output_tokens":2}}
`

const commandTurnJSONL = `{"type":"thread.started","thread_id":"thread-2"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc pwd","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc pwd","aggregated_output":"/tmp/project\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"/tmp/project"}}
{"type":"turn.completed","usage":{"input_tokens":20,"output_tokens":7}}
`

const todoTurnJSONL = `{"type":"thread.started","thread_id":"thread-3"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_0","type":"todo_list","items":[{"text":"inspect state","completed":true},{"text":"report result","completed":false}]}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"done"}}
{"type":"item.completed","item":{"id":"item_0","type":"todo_list","items":[{"text":"inspect state","completed":true},{"text":"report result","completed":false}]}}
{"type":"turn.completed","usage":{"input_tokens":30,"output_tokens":9}}
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

func TestAgent_Run_SimpleText(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"})
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

	assert.Equal(t, "thread-1", events[0].SessionID,
		"agent_start carries the thread ID",
	)

	last := events[len(events)-1]
	require.Len(t, last.Messages, 1)
	assert.Equal(t, "Hello!", last.Messages[0].Text())
	assert.Equal(t, 10, last.Usage.Input)
	assert.Equal(t, 5, last.Usage.Output)
	assert.Equal(t, 3, last.Usage.CacheRead)
	assert.Equal(t, 2, last.Usage.Reasoning)
	assert.Equal(t, "thread-1", a.SessionID())
}

func TestAgent_Run_ForwardsPromptAndOptions(t *testing.T) {
	a := New(
		ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"},
		agent.WithSystemPrompt("Be terse."),
		WithCLIPath("/usr/local/bin/codex"),
		WithWorkDir("/repo"),
		WithAddDirs("/extra"),
		WithSandbox("read-only"),
		WithApprovalPolicy("never"),
		WithSkipGitRepoCheck(),
		WithIgnoreRules(),
	)
	calls, restore := stubRunner(a, simpleTurnJSONL)
	defer restore()
	defer a.Close()

	_, err := a.Run(context.Background(), ai.UserMessage("ping")).Wait()
	require.NoError(t, err)

	require.Len(t, calls(), 1)
	call := calls()[0]
	assert.Equal(t, "/usr/local/bin/codex", call.cfg.cliPath)
	assert.Equal(t, "gpt-5.4", call.cfg.model)
	assert.Equal(t, "/repo", call.cfg.workDir)
	assert.Equal(t, []string{"/extra"}, call.cfg.addDirs)
	assert.Equal(t, "read-only", call.cfg.sandbox)
	assert.Equal(t, "never", call.cfg.approvalPolicy)
	assert.True(t, call.cfg.skipGitRepoCheck)
	assert.True(t, call.cfg.ignoreRules)
	assert.Equal(t, "Be terse.\n\nping", call.args.prompt)
	assert.False(t, call.args.resume)
}

func TestAgent_Run_SecondTurnResumesSession(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"})
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
	assert.Equal(t, "thread-1", calls()[1].args.sessionID)
	assert.Equal(t, "thread-1", a.SessionID())
}

func TestAgent_Run_CommandExecutionEvents(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"})
	_, restore := stubRunner(a, commandTurnJSONL)
	defer restore()
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("run pwd")))
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
	assert.Equal(t, "item_0", toolEnd.ToolCallID)
	assert.Equal(t, "bash", toolEnd.ToolName)
	assert.Equal(t, "/tmp/project\n", toolEnd.Result)
	assert.False(t, toolEnd.IsError)

	require.NotNil(t, turnEnd)
	require.Len(t, turnEnd.ToolResults, 1)
	assert.Equal(t, ai.RoleToolResult, turnEnd.ToolResults[0].Role)
	assert.Equal(t, "/tmp/project\n", turnEnd.ToolResults[0].Text())
}

func TestAgent_Run_TodoListMessages(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"})
	_, restore := stubRunner(a, todoTurnJSONL)
	defer restore()
	defer a.Close()

	events, err := collectRun(t, a.Run(context.Background(), ai.UserMessage("make a plan")))
	require.NoError(t, err)

	var ended []ai.Message
	for i := range events {
		if events[i].Type != agent.EventMessageEnd || events[i].Message == nil {
			continue
		}
		ended = append(ended, *events[i].Message)
	}

	require.Len(t, ended, 3)

	toolCalls := ended[0].ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "item_0", toolCalls[0].ID)
	assert.Equal(t, "TodoWrite", toolCalls[0].Name)
	assert.Equal(t, []map[string]any{
		{
			"content":     "inspect state",
			"active_form": "inspect state",
			"status":      "completed",
		},
		{
			"content":     "report result",
			"active_form": "report result",
			"status":      "pending",
		},
	}, toolCalls[0].Arguments["todos"])

	assert.Equal(t, "done", ended[1].Text())
	assert.Equal(t, ai.RoleToolResult, ended[2].Role)
	assert.Equal(t, "item_0", ended[2].ToolCallID)
	assert.Equal(t, "TodoWrite", ended[2].ToolName)

	last := events[len(events)-1]
	require.Len(t, last.Messages, 3)
}

func TestAgent_Run_ConcurrentRejected(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"})
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

// Canceling the Run context terminates the Codex child; the run ends
// with context.Canceled and the agent returns to idle.
func TestAgent_Run_CancelKillsTurn(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"})
	reader, writer := io.Pipe()
	started := make(chan struct{})
	a.runFn = func(ctx context.Context, _ config, _ runArgs) (io.ReadCloser, func() error, error) {
		close(started)
		// Mirror the real transport: when the turn ctx is cancelled, the child
		// dies and stdout closes.
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

func TestAgent_Run_StartError(t *testing.T) {
	a := New(ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"})
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
	a := New(ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"})
	_, err := a.Run(context.Background()).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestNew_ComposesAgentAndCodexOptions(t *testing.T) {
	ca := New(
		ai.Model{ID: "gpt-5.4", Name: "gpt-5.4"},
		agent.WithMaxTurns(3),
		WithCLIPath("/bin/codex"),
		WithSessionID("thread-xyz"),
		WithSandbox("workspace-write"),
		WithThinkingLevel(ai.ThinkingHigh),
	)

	assert.Equal(t, "gpt-5.4", ca.cfg.model)
	assert.Equal(t, 3, ca.cfg.maxTurns)
	assert.Equal(t, "/bin/codex", ca.cfg.cliPath)
	assert.Equal(t, "thread-xyz", ca.sessionID)
	assert.Equal(t, "workspace-write", ca.cfg.sandbox)
	assert.Equal(t, ai.ThinkingHigh, ca.cfg.thinkingLevel)
}

func TestBuildArgs_ThinkingLevelMapsToReasoningEffort(t *testing.T) {
	got := buildArgs(
		config{
			cliPath:       "codex",
			model:         "gpt-5.4",
			thinkingLevel: ai.ThinkingHigh,
		},
		runArgs{prompt: "go"},
	)

	assert.Equal(t, []string{
		"--model", "gpt-5.4",
		"-c", "model_reasoning_effort=high",
		"exec",
		"--json",
		"--color", "never",
		"go",
	}, got)
}

func TestReasoningEffortForThinkingLevel(t *testing.T) {
	tests := []struct {
		level ai.ThinkingLevel
		want  string
	}{
		{level: "", want: ""},
		{level: ai.ThinkingOff, want: ""},
		{level: ai.ThinkingMinimal, want: "minimal"},
		{level: ai.ThinkingLow, want: "low"},
		{level: ai.ThinkingMedium, want: "medium"},
		{level: ai.ThinkingHigh, want: "high"},
		{level: ai.ThinkingXHigh, want: "xhigh"},
		{level: "bogus", want: ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			assert.Equal(t, tt.want, reasoningEffortForThinkingLevel(tt.level))
		})
	}
}

func TestBuildArgs_ResumeOmitsExecOnlyColorFlag(t *testing.T) {
	got := buildArgs(
		config{
			cliPath:        "codex",
			approvalPolicy: "never",
			model:          "gpt-5.4",
			workDir:        "/repo",
		},
		runArgs{
			prompt:    "next",
			resume:    true,
			sessionID: "thread-1",
		},
	)

	assert.Equal(t, []string{
		"--ask-for-approval", "never",
		"--model", "gpt-5.4",
		"--cd", "/repo",
		"exec",
		"resume",
		"--json",
		"thread-1",
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
