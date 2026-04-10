package openairesponses_test

import (
	"context"
	"encoding/json"
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

// weatherToolSchema is the JSON Schema for the get_weather tool.
var weatherToolSchema = func() *ai.ToolInfo {
	var schema map[string]any
	json.Unmarshal([]byte(`{
		"type": "object",
		"properties": {
			"location": {
				"type": "string",
				"description": "The city name"
			}
		},
		"required": ["location"]
	}`), &schema)
	return &ai.ToolInfo{
		Name:        "get_weather",
		Description: "Get the weather for a location",
	}
}()

const testModelID = "gpt-4o"

//go:generate go test -httprecord=Test

// scrubRequest normalizes requests for deterministic matching.
func scrubRequest(req *http.Request) error {
	req.Header.Del("Authorization")
	req.Header.Del("OpenAI-Organization")
	req.Header.Del("OpenAI-Project")
	req.Header.Del("Accept")
	for key := range req.Header {
		if strings.HasPrefix(key, "X-Stainless-") {
			req.Header.Del(key)
		}
	}
	req.Header.Set("User-Agent", "Go-http-client/1.1")
	return nil
}

// newTestProvider creates an OpenAI Responses provider configured for testing.
func newTestProvider(t *testing.T) (*aior.Provider, func()) {
	t.Helper()

	trace := filepath.Join(
		"testdata",
		strings.ReplaceAll(t.Name()+".httprr", "/", "_"),
	)

	if recording, _ := httprr.Recording(trace); !recording {
		if _, err := os.Stat(trace); os.IsNotExist(err) {
			t.Skipf(
				"httprr cassette not found: %s (run go generate to record)",
				trace,
			)
		}
	}

	rr, err := httprr.Open(trace, http.DefaultTransport)
	require.NoError(t, err, "failed to open httprr")
	rr.ScrubReq(scrubRequest)

	apiKey := "fake-api-key"
	if recording, _ := httprr.Recording(trace); recording {
		apiKey = os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			t.Skip("OPENAI_API_KEY not set, skipping recording")
		}
	}

	p := aior.New(
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(rr.Client()),
	)

	return p, func() { rr.Close() }
}

func testModel() ai.Model {
	return ai.Model{
		ID:       testModelID,
		Name:     testModelID,
		API:      "openai-responses",
		Provider: "openai",
	}
}

func TestGenerateText(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	model := testModel()

	maxTokens := 4000
	stream := p.StreamText(ctx, model, ai.Prompt{
		System: "You are a helpful assistant",
		Messages: []ai.Message{
			ai.UserMessage("Say hi in Portuguese"),
		},
	}, ai.StreamOptions{
		MaxTokens: &maxTokens,
	})

	msg, err := stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.NotEmpty(t, msg.Content, "expected at least one content block")

	var text string
	for _, c := range msg.Content {
		if tc, ok := ai.AsContent[ai.Text](c); ok {
			text += tc.Text
		}
	}

	expectedGreetings := []string{"Olá", "Oi", "olá", "oi"}
	found := false
	for _, greeting := range expectedGreetings {
		if strings.Contains(text, greeting) {
			found = true
			break
		}
	}
	assert.True(
		t, found,
		"response %q does not contain expected Portuguese greeting",
		text,
	)

	assert.Equal(t, ai.StopReasonStop, msg.StopReason)
}

func TestStreamText(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	model := testModel()

	maxTokens := 4000
	stream := p.StreamText(ctx, model, ai.Prompt{
		System: "You are a helpful assistant",
		Messages: []ai.Message{
			ai.UserMessage("Say hi in Portuguese"),
		},
	}, ai.StreamOptions{
		MaxTokens: &maxTokens,
	})

	var text strings.Builder
	var gotDone bool
	var finalMsg *ai.Message

	for e, err := range stream.Events() {
		require.NoError(t, err)
		switch e.Type {
		case ai.EventTextDelta:
			text.WriteString(e.Delta)
		case ai.EventDone:
			gotDone = true
			finalMsg = e.Message
		}
	}

	assert.True(t, gotDone, "expected done event")
	assert.NotEmpty(t, text.String(), "expected text output")
	require.NotNil(t, finalMsg)
	assert.Equal(t, ai.StopReasonStop, finalMsg.StopReason)
}

func TestStreamEventSequence(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	model := testModel()

	maxTokens := 100
	stream := p.StreamText(ctx, model, ai.Prompt{
		System: "You are a helpful assistant. Be very brief.",
		Messages: []ai.Message{
			ai.UserMessage("Say hello"),
		},
	}, ai.StreamOptions{
		MaxTokens: &maxTokens,
	})

	var events []ai.EventType
	for e, err := range stream.Events() {
		require.NoError(t, err)
		events = append(events, e.Type)
	}

	require.NotEmpty(t, events, "expected events")

	// Verify ordering: TextStart before TextDelta, TextEnd before Done
	textStartIdx := -1
	textEndIdx := -1
	doneIdx := -1

	for i, et := range events {
		switch et {
		case ai.EventTextStart:
			if textStartIdx == -1 {
				textStartIdx = i
			}
		case ai.EventTextEnd:
			textEndIdx = i
		case ai.EventDone:
			doneIdx = i
		}
	}

	if textStartIdx >= 0 {
		assert.Less(t, textStartIdx, textEndIdx, "TextStart should come before TextEnd")
	}
	assert.Less(t, textEndIdx, doneIdx, "TextEnd should come before Done")
}

func TestToolCall(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	model := testModel()

	maxTokens := 4000
	stream := p.StreamText(ctx, model, ai.Prompt{
		System: "You are a helpful assistant. Use the get_weather tool to answer weather questions.",
		Messages: []ai.Message{
			ai.UserMessage("What's the weather in NYC?"),
		},
		Tools: []ai.ToolInfo{*weatherToolSchema},
	}, ai.StreamOptions{
		MaxTokens: &maxTokens,
	})

	msg, err := stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)

	toolCalls := msg.ToolCalls()
	require.NotEmpty(t, toolCalls, "expected at least one tool call")

	tc := toolCalls[0]
	assert.Equal(t, "get_weather", tc.Name)
	assert.NotEmpty(t, tc.ID, "tool call should have an ID")
	assert.Equal(t, ai.StopReasonToolUse, msg.StopReason)
}

func TestToolCallMultiTurn(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	model := testModel()

	maxTokens := 4000

	// First turn: model calls the tool
	stream := p.StreamText(ctx, model, ai.Prompt{
		System: "You are a helpful assistant. Use the get_weather tool to answer weather questions.",
		Messages: []ai.Message{
			ai.UserMessage("What's the weather in NYC?"),
		},
		Tools: []ai.ToolInfo{*weatherToolSchema},
	}, ai.StreamOptions{
		MaxTokens: &maxTokens,
	})

	msg, err := stream.Result()
	require.NoError(t, err)

	toolCalls := msg.ToolCalls()
	require.NotEmpty(t, toolCalls)

	// Second turn: provide tool result
	stream = p.StreamText(ctx, model, ai.Prompt{
		System: "You are a helpful assistant. Use the get_weather tool to answer weather questions.",
		Messages: []ai.Message{
			ai.UserMessage("What's the weather in NYC?"),
			*msg,
			ai.ToolResultMessage(
				toolCalls[0].ID,
				toolCalls[0].Name,
				ai.Text{Text: `{"temperature": 72, "condition": "sunny"}`},
			),
		},
		Tools: []ai.ToolInfo{*weatherToolSchema},
	}, ai.StreamOptions{
		MaxTokens: &maxTokens,
	})

	msg, err = stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)

	text := msg.Text()
	assert.NotEmpty(t, text, "expected text response after tool result")
	assert.Equal(t, ai.StopReasonStop, msg.StopReason)
}

func TestUsageTokens(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	model := testModel()

	maxTokens := 100
	stream := p.StreamText(ctx, model, ai.Prompt{
		System: "You are a helpful assistant",
		Messages: []ai.Message{
			ai.UserMessage("Say hello"),
		},
	}, ai.StreamOptions{
		MaxTokens: &maxTokens,
	})

	msg, err := stream.Result()
	require.NoError(t, err)
	require.NotNil(t, msg)

	assert.NotZero(t, msg.Usage.Input, "expected non-zero input tokens")
	assert.NotZero(t, msg.Usage.Output, "expected non-zero output tokens")
	assert.NotZero(t, msg.Usage.Total, "expected non-zero total tokens")
}

func TestContextCancellation(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	model := testModel()

	stream := p.StreamText(ctx, model, ai.Prompt{
		Messages: []ai.Message{
			ai.UserMessage("This should be cancelled"),
		},
	}, ai.StreamOptions{})

	_, err := stream.Result()
	assert.Error(t, err)
}
