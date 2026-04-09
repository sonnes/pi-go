package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ---

const simpleTextNDJSON = `{"type":"system","subtype":"init","session_id":"sess-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello!"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}}
{"type":"result","subtype":"success","result":"Hello!","session_id":"sess-1","usage":{"input_tokens":10,"output_tokens":5}}
`

const toolCallNDJSON = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me read that."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/foo"}}],"stop_reason":"tool_use"}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"package main"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"It's a Go file."}],"stop_reason":"end_turn","usage":{"input_tokens":30,"output_tokens":10}}}
{"type":"result","subtype":"success","result":"It's a Go file.","usage":{"input_tokens":100,"output_tokens":30}}
`

const thinkingNDJSON = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Let me ponder..."},{"type":"text","text":"The answer is 42."}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"The answer is 42."}
`

const objectResultNDJSON = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"name\":\"Alice\",\"age\":30}"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20}}}
{"type":"result","subtype":"success","result":"{\"name\":\"Alice\",\"age\":30}","usage":{"input_tokens":10,"output_tokens":20}}
`

// --- stubs ---

// stubSend replaces Provider.sendFn with a fake that returns canned
// NDJSON and records the arguments it was called with.
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

// --- interface compliance ---

func TestProvider_ImplementsInterfaces(t *testing.T) {
	var (
		_ ai.Provider       = (*Provider)(nil)
		_ ai.ObjectProvider = (*Provider)(nil)
	)
}

func TestProvider_API(t *testing.T) {
	p := New()
	assert.Equal(t, "claude-cli", p.API())
}

// --- StreamText ---

func TestStreamText_SimpleText(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	stream := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	)

	var events []ai.Event
	for evt, err := range stream.Events() {
		require.NoError(t, err)
		events = append(events, evt)
	}

	types := aiEventTypes(events)
	assert.Equal(t, []ai.EventType{
		ai.EventStart,
		ai.EventTextStart,
		ai.EventTextDelta,
		ai.EventTextEnd,
		ai.EventDone,
	}, types)

	assert.Equal(t, "Hello!", events[2].Delta)
	assert.Equal(t, "Hello!", events[3].Content)

	last := events[len(events)-1]
	require.NotNil(t, last.Message)
	assert.Equal(t, "Hello!", last.Message.Text())
	assert.Equal(t, 10, last.Message.Usage.Input)
	assert.Equal(t, 5, last.Message.Usage.Output)
}

func TestStreamText_ResultPath(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	msg, err := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	).Result()
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "Hello!", msg.Text())
}

func TestStreamText_ToolCall(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, toolCallNDJSON, nil)
	defer restore()

	stream := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("read /tmp/foo")}},
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
	assert.Equal(t, "t1", toolEnd.ToolCall.ID)
	assert.Equal(t, "Read", toolEnd.ToolCall.Name)
	assert.Equal(t, "/tmp/foo", toolEnd.ToolCall.Arguments["file_path"])

	last := events[len(events)-1]
	require.Equal(t, ai.EventDone, last.Type)
	require.NotNil(t, last.Message)
	assert.Equal(t, "It's a Go file.", last.Message.Text())
}

func TestStreamText_Thinking(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, thinkingNDJSON, nil)
	defer restore()

	stream := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("what is 6*7?")}},
		ai.StreamOptions{},
	)

	var events []ai.Event
	for evt, err := range stream.Events() {
		require.NoError(t, err)
		events = append(events, evt)
	}

	types := aiEventTypes(events)
	assert.Equal(t, []ai.EventType{
		ai.EventStart,
		ai.EventThinkStart,
		ai.EventThinkDelta,
		ai.EventThinkEnd,
		ai.EventTextStart,
		ai.EventTextDelta,
		ai.EventTextEnd,
		ai.EventDone,
	}, types)

	var thinkDelta *ai.Event
	for i := range events {
		if events[i].Type == ai.EventThinkDelta {
			thinkDelta = &events[i]
			break
		}
	}
	require.NotNil(t, thinkDelta)
	assert.Equal(t, "Let me ponder...", thinkDelta.Delta)
}

func TestStreamText_EmptyPrompt(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, simpleTextNDJSON, nil)
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

func TestStreamText_ErrorResult(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"result","subtype":"error","result":"Rate limited","is_error":true}
`
	p := New()
	_, _, restore := stubSend(p, ndjson, nil)
	defer restore()

	_, err := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	).Result()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rate limited")
}

func TestStreamText_ForwardsNoPersistence(t *testing.T) {
	p := New()
	lastArgs, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	_, _ = p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	).Result()

	args := lastArgs()
	assert.True(t, args.noPersistence)
	assert.Equal(t, "hi", args.prompt)
}

func TestStreamText_ForwardsSystemPrompt(t *testing.T) {
	p := New()
	lastArgs, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	prompt := ai.Prompt{
		System:   "be terse",
		Messages: []ai.Message{ai.UserMessage("hi")},
	}
	_, _ = p.StreamText(context.Background(), ai.Model{}, prompt, ai.StreamOptions{}).Result()

	assert.Equal(t, "be terse", lastArgs().systemPrompt)
}

func TestStreamText_ModelOverride(t *testing.T) {
	p := New(WithModel("default-model"))
	_, lastCfg, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	_, _ = p.StreamText(
		context.Background(),
		ai.Model{ID: "override"},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	).Result()

	assert.Equal(t, "override", lastCfg().model)
}

func TestStreamText_UsesLastUserMessage(t *testing.T) {
	p := New()
	lastArgs, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	prompt := ai.Prompt{
		Messages: []ai.Message{
			ai.UserMessage("first"),
			ai.AssistantMessage(ai.Text{Text: "response"}),
			ai.UserMessage("latest"),
		},
	}
	_, _ = p.StreamText(context.Background(), ai.Model{}, prompt, ai.StreamOptions{}).Result()

	assert.Equal(t, "latest", lastArgs().prompt)
}

func TestStreamText_ConcurrentCalls(t *testing.T) {
	p := New()
	p.sendFn = func(_ context.Context, _ config, _ sendArgs) (io.ReadCloser, func() error, error) {
		return io.NopCloser(strings.NewReader(simpleTextNDJSON)), func() error { return nil }, nil
	}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	msgs := make([]*ai.Message, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msgs[i], errs[i] = p.StreamText(
				context.Background(),
				ai.Model{},
				ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
				ai.StreamOptions{},
			).Result()
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i], "call %d", i)
		require.NotNil(t, msgs[i], "call %d", i)
		assert.Equal(t, "Hello!", msgs[i].Text(), "call %d", i)
	}
}

// --- GenerateObject ---

func TestGenerateObject_Success(t *testing.T) {
	p := New()
	lastArgs, _, restore := stubSend(p, objectResultNDJSON, nil)
	defer restore()

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
	assert.Equal(t, 10, resp.Usage.Input)
	assert.Equal(t, 20, resp.Usage.Output)

	args := lastArgs()
	assert.NotEmpty(t, args.jsonSchema)
	var schemaMap map[string]any
	require.NoError(t, json.Unmarshal([]byte(args.jsonSchema), &schemaMap))
	assert.Equal(t, "object", schemaMap["type"])
	assert.True(t, args.noPersistence)
}

func TestGenerateObject_ViaGenericHelper(t *testing.T) {
	type user struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	p := New()
	_, _, restore := stubSend(p, objectResultNDJSON, nil)
	defer restore()

	ai.RegisterProvider("claude-cli-test", p)
	defer ai.UnregisterProvider("claude-cli-test")

	result, err := ai.GenerateObject[user](
		context.Background(),
		ai.Model{API: "claude-cli-test"},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("give me a user")}},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Alice", result.Object.Name)
	assert.Equal(t, 30, result.Object.Age)
}

func TestGenerateObject_EmptyPrompt(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, objectResultNDJSON, nil)
	defer restore()

	_, err := p.GenerateObject(
		context.Background(),
		ai.Model{},
		ai.Prompt{},
		userSchema(t),
		ai.StreamOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no user message")
}

func TestGenerateObject_NilSchema(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, objectResultNDJSON, nil)
	defer restore()

	_, err := p.GenerateObject(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		nil,
		ai.StreamOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema is required")
}

func TestGenerateObject_SubprocessError(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, "", fmt.Errorf("cli not found"))
	defer restore()

	_, err := p.GenerateObject(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		userSchema(t),
		ai.StreamOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli not found")
}

func TestGenerateObject_ErrorResult(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"result","subtype":"error","result":"Schema validation failed","is_error":true}
`
	p := New()
	_, _, restore := stubSend(p, ndjson, nil)
	defer restore()

	_, err := p.GenerateObject(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		userSchema(t),
		ai.StreamOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Schema validation failed")
}

func TestGenerateObject_FallsBackToAssistantText(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"name\":\"Bob\",\"age\":25}"}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success"}
`
	p := New()
	_, _, restore := stubSend(p, ndjson, nil)
	defer restore()

	resp, err := p.GenerateObject(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		userSchema(t),
		ai.StreamOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, `{"name":"Bob","age":25}`, resp.Raw)
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

// --- buildArgs ---

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		args sendArgs
		want []string
	}{
		{
			name: "minimal provider mode",
			cfg:  config{cliPath: "claude"},
			args: sendArgs{prompt: "hi", noPersistence: true},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--no-session-persistence",
				"hi",
			},
		},
		{
			name: "with model, system prompt and schema",
			cfg:  config{cliPath: "claude", model: "sonnet"},
			args: sendArgs{
				prompt:        "give me a user",
				systemPrompt:  "be terse",
				jsonSchema:    `{"type":"object"}`,
				noPersistence: true,
			},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--no-session-persistence",
				"--model", "sonnet",
				"--system-prompt", "be terse",
				"--json-schema", `{"type":"object"}`,
				"give me a user",
			},
		},
		{
			name: "with add-dirs and allowed tools",
			cfg: config{
				cliPath:      "claude",
				allowedTools: []string{"Read", "Edit"},
				addDirs:      []string{"/extra"},
				maxTurns:     3,
			},
			args: sendArgs{prompt: "go", noPersistence: true},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--no-session-persistence",
				"--allowedTools", "Read,Edit",
				"--max-turns", "3",
				"--add-dir", "/extra",
				"go",
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

// --- helpers ---

func aiEventTypes(events []ai.Event) []ai.EventType {
	types := make([]ai.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}
