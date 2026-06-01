package codexcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const simpleTextJSONL = `Reading additional input from stdin...
{"type":"thread.started","thread_id":"thread-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Hello!"}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":3,"output_tokens":5,"reasoning_output_tokens":2}}
`

const commandJSONL = `{"type":"thread.started","thread_id":"thread-1"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc pwd","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc pwd","aggregated_output":"/tmp/project\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"/tmp/project"}}
{"type":"turn.completed","usage":{"input_tokens":20,"output_tokens":7}}
`

const objectJSONL = `{"type":"thread.started","thread_id":"thread-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"{\"name\":\"Alice\",\"age\":30}"}}
{"type":"turn.completed","usage":{"input_tokens":11,"output_tokens":9}}
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

func userSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	schema, err := jsonschema.For[struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}](nil)
	require.NoError(t, err)
	return schema
}

func TestProvider_ImplementsInterfaces(t *testing.T) {
	var (
		_ ai.Provider       = (*Provider)(nil)
		_ ai.ObjectProvider = (*Provider)(nil)
	)
}

func TestProvider_API(t *testing.T) {
	p := New()
	assert.Equal(t, "codex-cli", p.API())
}

func TestStreamText_SimpleText(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, simpleTextJSONL, nil)
	defer restore()

	stream := p.StreamText(
		context.Background(),
		ai.Model{ID: "gpt-5.4"},
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
		ai.EventTextEnd,
		ai.EventDone,
	}, aiEventTypes(events))

	assert.Equal(t, "Hello!", events[2].Delta)
	last := events[len(events)-1]
	require.NotNil(t, last.Message)
	assert.Equal(t, "Hello!", last.Message.Text())
	assert.Equal(t, "codex-cli", last.Message.API)
	assert.Equal(t, "gpt-5.4", last.Message.Model)
	assert.Equal(t, 10, last.Message.Usage.Input)
	assert.Equal(t, 5, last.Message.Usage.Output)
	assert.Equal(t, 3, last.Message.Usage.CacheRead)
	assert.Equal(t, 2, last.Message.Usage.Reasoning)
	assert.Equal(t, 15, last.Message.Usage.Total)
}

func TestStreamText_CommandExecution(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, commandJSONL, nil)
	defer restore()

	stream := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("run pwd")}},
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
	assert.Equal(t, "item_0", toolEnd.ToolCall.ID)
	assert.Equal(t, "bash", toolEnd.ToolCall.Name)
	assert.True(t, toolEnd.ToolCall.Server)
	assert.Equal(t, ai.ServerToolBash, toolEnd.ToolCall.ServerType)
	assert.Equal(t, "/bin/zsh -lc pwd", toolEnd.ToolCall.Arguments["command"])
	require.NotNil(t, toolEnd.ToolCall.Output)
	assert.Equal(t, "/tmp/project\n", toolEnd.ToolCall.Output.Content)
	assert.False(t, toolEnd.ToolCall.Output.IsError)

	last := events[len(events)-1]
	require.NotNil(t, last.Message)
	assert.Equal(t, "/tmp/project", last.Message.Text())
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

func TestGenerateObject_Success(t *testing.T) {
	p := New()

	var schemaText string
	var captured sendArgs
	p.sendFn = func(_ context.Context, cfg config, args sendArgs) (io.ReadCloser, func() error, error) {
		captured = args
		data, err := os.ReadFile(args.outputSchemaPath)
		require.NoError(t, err)
		schemaText = string(data)
		return io.NopCloser(strings.NewReader(objectJSONL)), func() error { return nil }, nil
	}

	resp, err := p.GenerateObject(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("give me a user")}},
		userSchema(t),
		ai.StreamOptions{},
	)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, `{"name":"Alice","age":30}`, resp.Raw)
	assert.Equal(t, 11, resp.Usage.Input)
	assert.Equal(t, 9, resp.Usage.Output)
	assert.NotEmpty(t, captured.outputSchemaPath)

	var schemaMap map[string]any
	require.NoError(t, json.Unmarshal([]byte(schemaText), &schemaMap))
	assert.Equal(t, "object", schemaMap["type"])
}

func TestGenerateObject_EmptyOutput(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, "", nil)
	defer restore()

	_, err := p.GenerateObject(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		userSchema(t),
		ai.StreamOptions{},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty object result")
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		args sendArgs
		want []string
	}{
		{
			name: "minimal provider mode",
			cfg:  config{cliPath: "codex", approvalPolicy: "never"},
			args: sendArgs{prompt: "hi", ephemeral: true},
			want: []string{
				"--ask-for-approval", "never",
				"exec",
				"--json",
				"--color", "never",
				"--ephemeral",
				"hi",
			},
		},
		{
			name: "with model workdir sandbox and schema",
			cfg: config{
				cliPath:          "codex",
				approvalPolicy:   "on-request",
				model:            "gpt-5.4",
				workDir:          "/repo",
				addDirs:          []string{"/extra"},
				sandbox:          "read-only",
				skipGitRepoCheck: true,
				ignoreUserConfig: true,
				ignoreRules:      true,
			},
			args: sendArgs{
				prompt:           "json please",
				ephemeral:        true,
				outputSchemaPath: "/tmp/schema.json",
			},
			want: []string{
				"--ask-for-approval", "on-request",
				"exec",
				"--json",
				"--color", "never",
				"--ephemeral",
				"--model", "gpt-5.4",
				"--cd", "/repo",
				"--add-dir", "/extra",
				"--sandbox", "read-only",
				"--skip-git-repo-check",
				"--ignore-user-config",
				"--ignore-rules",
				"--output-schema", "/tmp/schema.json",
				"json please",
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
