package openairesponses_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openai/openai-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/internal/httprr"
	ai "github.com/sonnes/pi-go/pkg/ai"
	aior "github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
)

// Recording: run with `go test -httprecord=TestOpenRouter ./pkg/ai/provider/openairesponses/`
// after exporting OPENROUTER_API_KEY. Each test below produces a cassette in
// testdata/ that subsequent runs replay deterministically.

const (
	openRouterBaseURL    = "https://openrouter.ai/api/v1"
	openRouterModelGPT   = "openai/gpt-4o-mini"
	openRouterModelClaud = "anthropic/claude-haiku-4-5"
)

// scrubOpenRouterRequest normalizes OpenRouter-specific headers so cassettes
// don't leak credentials and stay deterministic across machines.
func scrubOpenRouterRequest(req *http.Request) error {
	req.Header.Del("Authorization")
	req.Header.Del("HTTP-Referer")
	req.Header.Del("X-Title")
	for key := range req.Header {
		if strings.HasPrefix(key, "X-Stainless-") {
			req.Header.Del(key)
		}
	}
	req.Header.Set("User-Agent", "Go-http-client/1.1")
	return nil
}

func newOpenRouterTestProvider(t *testing.T) (*aior.Provider, func()) {
	t.Helper()

	trace := filepath.Join(
		"testdata",
		strings.ReplaceAll(t.Name()+".httprr", "/", "_"),
	)

	if recording, _ := httprr.Recording(trace); !recording {
		if _, err := os.Stat(trace); os.IsNotExist(err) {
			t.Skipf(
				"httprr cassette not found: %s (record with -httprecord=%s and OPENROUTER_API_KEY)",
				trace,
				t.Name(),
			)
		}
	}

	rr, err := httprr.Open(trace, http.DefaultTransport)
	require.NoError(t, err, "failed to open httprr")
	rr.ScrubReq(scrubOpenRouterRequest)

	apiKey := "fake-openrouter-key"
	if recording, _ := httprr.Recording(trace); recording {
		apiKey = os.Getenv("OPENROUTER_API_KEY")
		if apiKey == "" {
			t.Skip("OPENROUTER_API_KEY not set, skipping recording")
		}
	}

	p := aior.NewForOpenRouter(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(openRouterBaseURL),
		option.WithHTTPClient(rr.Client()),
	)

	return p, func() { rr.Close() }
}

func openRouterModel(id string) ai.Model {
	return ai.Model{
		ID:       id,
		Name:     id,
		API:      "openai-responses",
		Provider: "openrouter",
	}
}

// TestOpenRouter_GenerateText confirms a basic text turn streams cleanly
// through the dialect's content_part.delta handlers.
func TestOpenRouter_GenerateText(t *testing.T) {
	p, cleanup := newOpenRouterTestProvider(t)
	defer cleanup()

	maxTokens := 200
	stream := p.StreamText(context.Background(), openRouterModel(openRouterModelGPT), ai.Prompt{
		System: "You are a helpful assistant. Be brief.",
		Messages: []ai.Message{
			ai.UserMessage("Say hi in one word."),
		},
	}, ai.StreamOptions{MaxTokens: &maxTokens})

	msg, err := stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)

	var text string
	for _, c := range msg.Content {
		if tc, ok := ai.AsContent[ai.Text](c); ok {
			text += tc.Text
		}
	}
	assert.NotEmpty(t, text, "expected text content")
	assert.Equal(t, ai.StopReasonStop, msg.StopReason)
}

// TestOpenRouter_StreamEventOrdering confirms TextStart/TextDelta/TextEnd
// fire in the right order under the OpenRouter SSE taxonomy.
func TestOpenRouter_StreamEventOrdering(t *testing.T) {
	p, cleanup := newOpenRouterTestProvider(t)
	defer cleanup()

	maxTokens := 100
	stream := p.StreamText(context.Background(), openRouterModel(openRouterModelGPT), ai.Prompt{
		System: "Be brief.",
		Messages: []ai.Message{
			ai.UserMessage("Say hello"),
		},
	}, ai.StreamOptions{MaxTokens: &maxTokens})

	var events []ai.EventType
	for e, err := range stream.Events() {
		require.NoError(t, err)
		events = append(events, e.Type)
	}
	require.NotEmpty(t, events)

	startIdx, deltaIdx, endIdx, doneIdx := -1, -1, -1, -1
	for i, et := range events {
		switch et {
		case ai.EventTextStart:
			if startIdx < 0 {
				startIdx = i
			}
		case ai.EventTextDelta:
			if deltaIdx < 0 {
				deltaIdx = i
			}
		case ai.EventTextEnd:
			endIdx = i
		case ai.EventDone:
			doneIdx = i
		}
	}
	assert.GreaterOrEqual(t, startIdx, 0, "expected TextStart")
	assert.Greater(t, deltaIdx, startIdx, "expected TextDelta after TextStart")
	assert.Greater(t, endIdx, deltaIdx, "expected TextEnd after TextDelta")
	assert.Greater(t, doneIdx, endIdx, "expected Done after TextEnd")
}

// TestOpenRouter_FunctionTool confirms function-tool calls are issued and
// streamed through unchanged — function-tool wire shape is identical
// across dialects.
func TestOpenRouter_FunctionTool(t *testing.T) {
	p, cleanup := newOpenRouterTestProvider(t)
	defer cleanup()

	weatherTool := ai.DefineParallelTool(
		"get_weather",
		"Get the weather for a city",
		func(_ context.Context, in struct {
			Location string `json:"location"`
		}) (string, error) {
			return "", nil
		},
	)

	maxTokens := 200
	stream := p.StreamText(context.Background(), openRouterModel(openRouterModelGPT), ai.Prompt{
		System: "Use the get_weather tool when asked about weather.",
		Messages: []ai.Message{
			ai.UserMessage("What's the weather in NYC?"),
		},
		Tools: []ai.ToolInfo{weatherTool.Info()},
	}, ai.StreamOptions{MaxTokens: &maxTokens})

	msg, err := stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)

	var sawCall bool
	for _, c := range msg.Content {
		if call, ok := ai.AsContent[ai.ToolCall](c); ok && !call.Server {
			sawCall = true
			assert.Equal(t, "get_weather", call.Name)
			assert.Contains(t, call.Arguments, "location")
		}
	}
	assert.True(t, sawCall, "expected the model to invoke get_weather")
}

// TestOpenRouter_ServerWebSearch records and replays a turn that uses
// openrouter:web_search. The cassette exercises:
//   - WithJSONSet injection of the openrouter:* tool into the request body
//   - response.output_item.added/.done with type starting "openrouter:"
//   - serverToolCalls accumulation and emission in the final ai.Message
func TestOpenRouter_ServerWebSearch(t *testing.T) {
	p, cleanup := newOpenRouterTestProvider(t)
	defer cleanup()

	tools := []ai.ToolInfo{
		{
			Name:       "web_search",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolWebSearch,
			ServerConfig: map[string]any{
				"max_results": 3,
			},
		},
	}

	maxTokens := 600
	stream := p.StreamText(context.Background(), openRouterModel(openRouterModelGPT), ai.Prompt{
		System: "Use web_search to ground your answer with recent sources.",
		Messages: []ai.Message{
			ai.UserMessage("What was Anthropic's most recent product launch?"),
		},
		Tools: tools,
	}, ai.StreamOptions{MaxTokens: &maxTokens})

	msg, err := stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)

	var sawServerCall bool
	for _, c := range msg.Content {
		if call, ok := ai.AsContent[ai.ToolCall](c); ok && call.Server {
			sawServerCall = true
			assert.Equal(t, ai.ServerToolWebSearch, call.ServerType)
			assert.Equal(t, "openrouter:web_search", call.Name)
			require.NotNil(t, call.Output)
			assert.NotEmpty(t, call.Output.Raw, "expected raw payload")
		}
	}
	assert.True(t, sawServerCall, "expected an openrouter:web_search server-tool call")
}

// TestOpenRouter_ServerDateTime exercises the simplest server tool —
// useful as a smoke test for dialect plumbing without depending on
// nondeterministic web search.
func TestOpenRouter_ServerDateTime(t *testing.T) {
	p, cleanup := newOpenRouterTestProvider(t)
	defer cleanup()

	tools := []ai.ToolInfo{
		{
			Name:       "datetime",
			Kind:       ai.ToolKindServer,
			ServerType: ai.ServerToolDateTime,
			ServerConfig: map[string]any{
				"timezone": "America/Los_Angeles",
			},
		},
	}

	maxTokens := 200
	stream := p.StreamText(context.Background(), openRouterModel(openRouterModelGPT), ai.Prompt{
		System: "Use the datetime tool to answer.",
		Messages: []ai.Message{
			ai.UserMessage("What's the current time in LA?"),
		},
		Tools: tools,
	}, ai.StreamOptions{MaxTokens: &maxTokens})

	msg, err := stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)

	var sawDateTime bool
	for _, c := range msg.Content {
		if call, ok := ai.AsContent[ai.ToolCall](c); ok && call.Server {
			if call.ServerType == ai.ServerToolDateTime {
				sawDateTime = true
			}
		}
	}
	assert.True(t, sawDateTime, "expected an openrouter:datetime server-tool call")
}

// TestOpenRouter_ClaudeUnderlying confirms the dialect works when the
// underlying model routed by OpenRouter is non-OpenAI. This is the whole
// point of routing through OpenRouter: same wire shape, different model.
func TestOpenRouter_ClaudeUnderlying(t *testing.T) {
	p, cleanup := newOpenRouterTestProvider(t)
	defer cleanup()

	maxTokens := 200
	stream := p.StreamText(context.Background(), openRouterModel(openRouterModelClaud), ai.Prompt{
		System: "Be brief.",
		Messages: []ai.Message{
			ai.UserMessage("Say hi in one word."),
		},
	}, ai.StreamOptions{MaxTokens: &maxTokens})

	msg, err := stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)

	var text string
	for _, c := range msg.Content {
		if tc, ok := ai.AsContent[ai.Text](c); ok {
			text += tc.Text
		}
	}
	assert.NotEmpty(t, text)
	assert.Equal(t, ai.StopReasonStop, msg.StopReason)
}
