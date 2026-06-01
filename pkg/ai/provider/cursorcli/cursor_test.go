package cursorcli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const simpleTextJSONL = `{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/repo","session_id":"session-1","model":"GPT-5","permissionMode":"default"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hel"}]},"session_id":"session-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"lo!"}]},"session_id":"session-1"}
{"type":"result","subtype":"success","is_error":false,"duration_ms":123,"duration_api_ms":100,"result":"Hello!","session_id":"session-1"}
`

const toolJSONL = `{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/repo","session_id":"session-1","model":"GPT-5","permissionMode":"default"}
{"type":"tool_call","subtype":"started","call_id":"tool-1","tool_call":{"readToolCall":{"args":{"path":"README.md"}}},"session_id":"session-1"}
{"type":"tool_call","subtype":"completed","call_id":"tool-1","tool_call":{"readToolCall":{"args":{"path":"README.md"},"result":{"success":{"content":"# Project\n","isEmpty":false,"totalLines":1,"totalChars":10}}}},"session_id":"session-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Read it."}]},"session_id":"session-1"}
{"type":"result","subtype":"success","is_error":false,"duration_ms":80,"duration_api_ms":70,"result":"Read it.","session_id":"session-1"}
`

func stubSend(
	p *Provider,
	output string,
	sendErr error,
) (lastArgs func() sendArgs, lastCfg func() config, restore func()) {
	var (
		mu       sync.Mutex
		captured sendArgs
		capCfg   config
	)
	orig := p.sendFn
	p.sendFn = func(_ context.Context, cfg config, args sendArgs) (io.ReadCloser, func() error, error) {
		mu.Lock()
		captured = args
		capCfg = cfg
		mu.Unlock()
		if sendErr != nil {
			return nil, nil, sendErr
		}
		return io.NopCloser(strings.NewReader(output)), func() error { return nil }, nil
	}
	return func() sendArgs {
			mu.Lock()
			defer mu.Unlock()
			return captured
		}, func() config {
			mu.Lock()
			defer mu.Unlock()
			return capCfg
		}, func() { p.sendFn = orig }
}

func TestProvider_ImplementsProvider(t *testing.T) {
	var _ ai.Provider = (*Provider)(nil)
}

func TestProvider_API(t *testing.T) {
	p := New()
	assert.Equal(t, "cursor-cli", p.API())
}

func TestStreamText_SimpleText(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, simpleTextJSONL, nil)
	defer restore()

	stream := p.StreamText(
		context.Background(),
		ai.Model{ID: "gpt-5"},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	)

	var events []ai.Event
	for evt, err := range stream.Events() {
		require.NoError(t, err)
		events = append(events, evt)
	}

	assert.Equal(t, []ai.EventType{
		ai.EventStart,
		ai.EventTextStart,
		ai.EventTextDelta,
		ai.EventTextDelta,
		ai.EventTextEnd,
		ai.EventDone,
	}, aiEventTypes(events))

	assert.Equal(t, "Hel", events[2].Delta)
	assert.Equal(t, "lo!", events[3].Delta)
	last := events[len(events)-1]
	require.NotNil(t, last.Message)
	assert.Equal(t, "Hello!", last.Message.Text())
	assert.Equal(t, "cursor-cli", last.Message.API)
	assert.Equal(t, "gpt-5", last.Message.Model)
}

func TestStreamText_ToolCall(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, toolJSONL, nil)
	defer restore()

	stream := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("read README")}},
		ai.StreamOptions{},
	)

	var events []ai.Event
	for evt, err := range stream.Events() {
		require.NoError(t, err)
		events = append(events, evt)
	}

	var toolEnd *ai.Event
	for i := range events {
		if events[i].Type == ai.EventToolEnd {
			toolEnd = &events[i]
			break
		}
	}
	require.NotNil(t, toolEnd)
	require.NotNil(t, toolEnd.ToolCall)
	assert.Equal(t, "tool-1", toolEnd.ToolCall.ID)
	assert.Equal(t, "read", toolEnd.ToolCall.Name)
	assert.True(t, toolEnd.ToolCall.Server)
	assert.Equal(t, ai.ServerToolTextEditor, toolEnd.ToolCall.ServerType)
	assert.Equal(t, "README.md", toolEnd.ToolCall.Arguments["path"])
	require.NotNil(t, toolEnd.ToolCall.Output)
	assert.Equal(t, "# Project\n", toolEnd.ToolCall.Output.Content)
	assert.False(t, toolEnd.ToolCall.Output.IsError)

	last := events[len(events)-1]
	require.NotNil(t, last.Message)
	assert.Equal(t, "Read it.", last.Message.Text())
	require.Len(t, last.Message.Content, 2)
}

func TestStreamText_EmptyPrompt(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, simpleTextJSONL, nil)
	defer restore()

	_, err := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{},
		ai.StreamOptions{},
	).Result()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no user message")
}

func TestStreamText_SubprocessError(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, "", fmt.Errorf("cli not found"))
	defer restore()

	_, err := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	).Result()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli not found")
}

func TestStreamText_ModelOverride(t *testing.T) {
	p := New(WithModel("default-model"))
	_, lastCfg, restore := stubSend(p, simpleTextJSONL, nil)
	defer restore()

	_, _ = p.StreamText(
		context.Background(),
		ai.Model{ID: "override"},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	).Result()

	assert.Equal(t, "override", lastCfg().model)
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		args sendArgs
		want []string
	}{
		{
			name: "minimal provider mode is ask",
			cfg:  config{cliPath: "cursor-agent", mode: "ask"},
			args: sendArgs{prompt: "hi"},
			want: []string{
				"-p",
				"--output-format", "stream-json",
				"--mode", "ask",
				"hi",
			},
		},
		{
			name: "with model workspace sandbox api key and force",
			cfg: config{
				cliPath:     "cursor-agent",
				apiKey:      "key",
				headers:     []string{"X-Test: yes"},
				model:       "gpt-5",
				workDir:     "/repo",
				mode:        "plan",
				sandbox:     "enabled",
				force:       true,
				approveMCPs: true,
				browser:     true,
			},
			args: sendArgs{prompt: "inspect"},
			want: []string{
				"--api-key", "key",
				"-H", "X-Test: yes",
				"-p",
				"--output-format", "stream-json",
				"--model", "gpt-5",
				"--mode", "plan",
				"--sandbox", "enabled",
				"--workspace", "/repo",
				"--force",
				"--approve-mcps",
				"--browser",
				"inspect",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildArgs(tt.cfg, tt.args)
			assert.Equal(t, tt.want, got)
		})
	}
}

func aiEventTypes(events []ai.Event) []ai.EventType {
	types := make([]ai.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}
