package claudecli

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

const cachedUsageNDJSON = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","usage":{"input_tokens":800,"output_tokens":500,"cache_read_input_tokens":200,"cache_creation_input_tokens":100}}}
{"type":"result","subtype":"success","result":"Hi","usage":{"input_tokens":800,"output_tokens":500,"cache_read_input_tokens":200,"cache_creation_input_tokens":100}}
`

const thinkingNDJSON = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Let me ponder..."},{"type":"text","text":"The answer is 42."}],"stop_reason":"end_turn"}}
{"type":"result","subtype":"success","result":"The answer is 42."}
`

const partialTextNDJSON = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo!"}}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello!"}],"stop_reason":"end_turn"}}
{"type":"stream_event","event":{"type":"content_block_stop","index":0}}
{"type":"result","subtype":"success","result":"Hello!","usage":{"input_tokens":10,"output_tokens":5}}
`

const objectResultNDJSON = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"name\":\"Alice\",\"age\":30}"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20}}}
{"type":"result","subtype":"success","result":"{\"name\":\"Alice\",\"age\":30}","usage":{"input_tokens":10,"output_tokens":20}}
`

// --- stubs ---

// stubSend replaces Provider.sendFn with a fake function. The fake
// returns canned NDJSON and records the arguments of each call.
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
		_ ai.TextProvider   = (*Provider)(nil)
		_ ai.ObjectProvider = (*Provider)(nil)
	)
}

func TestProviderID(t *testing.T) {
	assert.Equal(t, "claude-cli", ID)
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
	}, types)

	assert.Equal(t, "Hello!", events[2].Delta)
	assert.Equal(t, "Hello!", events[3].Content)

	msg, err := stream.Wait()
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "Hello!", msg.Text())
	assert.Equal(t, 10, msg.Usage.Input)
	assert.Equal(t, 5, msg.Usage.Output)
	assert.Equal(t, "claude-cli", msg.Provider, "assistant message is tagged with its provider")
}

func TestStreamText_WaitPath(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	msg, err := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	).Wait()
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

	msg, err := stream.Wait()
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "It's a Go file.", msg.Text())
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

func TestStreamText_PartialMessages(t *testing.T) {
	p := New(WithPartialMessages())
	_, _, restore := stubSend(p, partialTextNDJSON, nil)
	defer restore()

	stream := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	)

	var events []ai.Event
	for event, err := range stream.Events() {
		require.NoError(t, err)
		events = append(events, event)
	}

	assert.Equal(t, []ai.EventType{
		ai.EventStart,
		ai.EventTextStart,
		ai.EventTextDelta,
		ai.EventTextDelta,
		ai.EventTextEnd,
	}, aiEventTypes(events))
	assert.Equal(t, "Hel", events[2].Delta)
	assert.Equal(t, "lo!", events[3].Delta)
	assert.Equal(t, "Hello!", events[4].Content)

	message, err := stream.Wait()
	require.NoError(t, err)
	assert.Equal(t, "Hello!", message.Text())
}

func TestStreamText_ResumesSession(t *testing.T) {
	p := New(WithSessionID("sess-1"))
	lastArgs, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	_, err := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("continue")}},
		ai.StreamOptions{},
	).Wait()
	require.NoError(t, err)
	assert.Equal(t, "sess-1", lastArgs().resumeSession)
	assert.False(t, lastArgs().noPersistence)
}

// ai.WithSessionID is a prompt-cache affinity key, not a CLI session
// handle. A resume on that key gives the subprocess an ID that it never
// issued. The key must therefore stay inert here.
func TestStreamText_IgnoresCacheSessionID(t *testing.T) {
	p := New()
	lastArgs, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	_, err := p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{SessionID: "cache-affinity-key"},
	).Wait()
	require.NoError(t, err)
	assert.Empty(t, lastArgs().resumeSession)
	assert.True(t, lastArgs().noPersistence)
}

func TestGenerateObject_ResumesSession(t *testing.T) {
	p := New(WithSessionID("sess-1"))
	lastArgs, _, restore := stubSend(p, objectResultNDJSON, nil)
	defer restore()

	_, err := p.GenerateObject(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("continue")}},
		&jsonschema.Schema{Type: "object"},
		ai.StreamOptions{SessionID: "cache-affinity-key"},
	)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", lastArgs().resumeSession)
	assert.False(t, lastArgs().noPersistence)
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
	).Wait()
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
	).Wait()
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
	).Wait()
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
	).Wait()

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
	_, _ = p.StreamText(context.Background(), ai.Model{}, prompt, ai.StreamOptions{}).Wait()

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
	).Wait()

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
	_, _ = p.StreamText(context.Background(), ai.Model{}, prompt, ai.StreamOptions{}).Wait()

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
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msgs[i], errs[i] = p.StreamText(
				context.Background(),
				ai.Model{},
				ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
				ai.StreamOptions{},
			).Wait()
		}(i)
	}
	wg.Wait()

	for i := range n {
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

	lm := p.LanguageModel(ai.Model{ID: "obj"})

	result, err := ai.GenerateObject[user](
		context.Background(),
		lm,
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
			name: "thinking level maps to effort",
			cfg:  config{cliPath: "claude", model: "opus"},
			args: sendArgs{prompt: "go", effort: "xhigh", noPersistence: true},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--no-session-persistence",
				"--model", "opus",
				"--effort", "xhigh",
				"go",
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
		{
			name: "with partial messages and resumed session",
			cfg:  config{cliPath: "claude", partialMessages: true},
			args: sendArgs{prompt: "continue", resumeSession: "sess-123"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--include-partial-messages",
				"--resume", "sess-123",
				"continue",
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

func TestStreamText_ForwardsThinkingEffort(t *testing.T) {
	p := New()
	lastArgs, _, restore := stubSend(p, simpleTextNDJSON, nil)
	defer restore()

	_, _ = p.StreamText(
		context.Background(),
		ai.Model{},
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{ThinkingLevel: ai.ThinkingHigh},
	).Wait()

	assert.Equal(t, "high", lastArgs().effort)
}

func TestEffortForThinkingLevel(t *testing.T) {
	tests := []struct {
		level ai.ThinkingLevel
		want  string
	}{
		{level: "", want: ""},
		{level: ai.ThinkingOff, want: ""},
		{level: ai.ThinkingMinimal, want: "low"},
		{level: ai.ThinkingLow, want: "low"},
		{level: ai.ThinkingMedium, want: "medium"},
		{level: ai.ThinkingHigh, want: "high"},
		{level: ai.ThinkingXHigh, want: "xhigh"},
		{level: ai.ThinkingMax, want: "max"},
		{level: "bogus", want: ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			assert.Equal(t, tt.want, effortForThinkingLevel(tt.level))
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

func TestStreamText_UsageCost(t *testing.T) {
	p := New()
	_, _, restore := stubSend(p, cachedUsageNDJSON, nil)
	defer restore()

	model := ai.Model{
		ID:   "sonnet",
		Cost: ai.Cost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
	}

	stream := p.StreamText(
		context.Background(),
		model,
		ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}},
		ai.StreamOptions{},
	)

	msg, err := stream.Wait()
	require.NoError(t, err)
	require.NotNil(t, msg)

	assert.Equal(t, 800, msg.Usage.Input)
	assert.Equal(t, 200, msg.Usage.CacheRead)
	assert.Equal(t, 100, msg.Usage.CacheWrite)

	// The CLI relays the usage block of Anthropic. It has four disjoint
	// kinds, and each kind bills once at its own rate.
	cost := msg.Usage.Cost
	assert.InDelta(t, 0.0024, cost.Input, 1e-9)
	assert.InDelta(t, 0.0075, cost.Output, 1e-9)
	assert.InDelta(t, 0.00006, cost.CacheRead, 1e-9)
	assert.InDelta(t, 0.000375, cost.CacheWrite, 1e-9)
}

func TestStreamText_ThinkingDefaultAndOff(t *testing.T) {
	model := ai.Model{
		ID:                   "claude-opus-5",
		ThinkingLevels:       []ai.ThinkingLevel{ai.ThinkingLow, ai.ThinkingHigh},
		DefaultThinkingLevel: ai.ThinkingHigh,
	}
	prompt := ai.Prompt{Messages: []ai.Message{ai.UserMessage("hi")}}

	tests := []struct {
		name      string
		requested ai.ThinkingLevel
		want      string
	}{
		{name: "unset takes the model default", requested: "", want: "high"},
		{name: "explicit off sends no effort", requested: ai.ThinkingOff, want: ""},
		{name: "request above the ceiling maps down", requested: ai.ThinkingMax, want: "high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			lastArgs, _, restore := stubSend(p, simpleTextNDJSON, nil)
			defer restore()

			_, _ = p.StreamText(
				context.Background(),
				model,
				prompt,
				ai.StreamOptions{ThinkingLevel: tt.requested},
			).Wait()

			assert.Equal(t, tt.want, lastArgs().effort)
		})
	}
}
