package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/internal/httprr"
	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
)

// weatherToolSchema is the JSON Schema for the get_weather tool.
var weatherToolSchema = func() *jsonschema.Schema {
	var s jsonschema.Schema
	json.Unmarshal([]byte(`{
		"type": "object",
		"properties": {
			"location": {
				"type": "string",
				"description": "The city name"
			}
		},
		"required": ["location"]
	}`), &s)
	return &s
}()

// testPerson is a test struct for object generation tests.
type testPerson struct {
	Name string `json:"name" jsonschema:"The person's name"`
	Age  int    `json:"age" jsonschema:"The person's age"`
	City string `json:"city" jsonschema:"The city where the person lives"`
}

const testModelID = "claude-sonnet-4-20250514"

var testModel = ai.Model{
	ID:       testModelID,
	Name:     "Claude Sonnet",
	API:      "anthropic-messages",
	Provider: "anthropic",
}

//go:generate go test -httprecord=Test

// cacheControlRegexp strips cache_control markers from JSON bodies so
// existing httprr fixtures (recorded before caching was added) still match
// production requests that now carry markers. Matches both {"type":"ephemeral"}
// and {"type":"ephemeral","ttl":"1h"} forms with a leading comma (which is
// always present because cache_control comes after "text" in the SDK's
// canonical marshal order).
var cacheControlRegexp = regexp.MustCompile(`,"cache_control":\{[^}]*\}`)

// scrubRequest normalizes requests for deterministic matching.
func scrubRequest(req *http.Request) error {
	req.Header.Del("x-api-key")
	req.Header.Del("Authorization")
	req.Header.Del("Accept")
	req.Header.Del("Anthropic-Version")
	for key := range req.Header {
		if strings.HasPrefix(key, "X-Stainless-") {
			req.Header.Del(key)
		}
	}
	req.Header.Set("User-Agent", "Go-http-client/1.1")

	if req.Body == nil {
		return nil
	}
	body, ok := req.Body.(*httprr.Body)
	if !ok {
		return nil
	}
	stripped := cacheControlRegexp.ReplaceAll(body.Data, nil)
	body.Data = stripped
	body.ReadOffset = 0
	req.ContentLength = int64(len(stripped))
	req.Header.Set("Content-Length", strconv.Itoa(len(stripped)))
	return nil
}

// newTestProvider creates an Anthropic provider configured for testing.
func newTestProvider(t *testing.T) (*anthropic.Provider, func()) {
	t.Helper()

	trace := filepath.Join(
		"testdata",
		strings.ReplaceAll(t.Name()+".httprr", "/", "_"),
	)

	if recording, _ := httprr.Recording(trace); !recording {
		if _, err := os.Stat(trace); os.IsNotExist(err) {
			t.Skipf("httprr cassette not found: %s (run go generate to record)", trace)
		}
	}

	rr, err := httprr.Open(trace, http.DefaultTransport)
	require.NoError(t, err, "failed to open httprr")

	rr.ScrubReq(scrubRequest)

	apiKey := "fake-api-key"
	if recording, _ := httprr.Recording(trace); recording {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			t.Skip("ANTHROPIC_API_KEY not set, skipping recording")
		}
	}

	p := anthropic.New(
		anthropic.WithAPIKey(apiKey),
		anthropic.WithHTTPClient(rr.Client()),
	)

	return p, func() { rr.Close() }
}

func TestGenerateText(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	msg, err := p.StreamText(
		context.Background(),
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant. Be concise.",
			Messages: []ai.Message{
				ai.UserMessage("Say hi in Portuguese"),
			},
		},
		ai.StreamOptions{},
	).Result()

	require.NoError(t, err)
	require.NotNil(t, msg)
	require.NotEmpty(t, msg.Content, "expected at least one content block")

	var text string
	for _, c := range msg.Content {
		if tc, ok := c.(ai.Text); ok {
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
	assert.True(t, found, "response %q does not contain expected Portuguese greeting", text)

	assert.NotZero(t, msg.Usage.Total, "expected non-zero total tokens")
	assert.Equal(t, ai.StopReasonStop, msg.StopReason)
	assert.Equal(t, ai.RoleAssistant, msg.Role)
}

func TestStreamText(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	stream := p.StreamText(
		context.Background(),
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant. Be concise.",
			Messages: []ai.Message{
				ai.UserMessage("Say hi in Portuguese"),
			},
		},
		ai.StreamOptions{},
	)

	var text strings.Builder
	var gotDone bool
	var finalMsg *ai.Message

	for ev, err := range stream.Events() {
		require.NoError(t, err)

		switch ev.Type {
		case ai.EventTextDelta:
			text.WriteString(ev.Delta)
		case ai.EventDone:
			gotDone = true
			finalMsg = ev.Message
		}
	}

	assert.True(t, gotDone, "did not receive done event")

	response := text.String()
	expectedGreetings := []string{"Olá", "Oi", "olá", "oi"}
	found := false
	for _, greeting := range expectedGreetings {
		if strings.Contains(response, greeting) {
			found = true
			break
		}
	}
	assert.True(t, found, "response %q does not contain expected Portuguese greeting", response)

	require.NotNil(t, finalMsg, "expected final message in done event")
	assert.NotZero(t, finalMsg.Usage.Total, "expected non-zero total tokens")
	assert.Equal(t, ai.StopReasonStop, finalMsg.StopReason)
}

func TestGenerateObject(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	schema, err := jsonschema.For[testPerson](nil)
	require.NoError(t, err)

	resp, err := p.GenerateObject(
		context.Background(),
		testModel,
		ai.Prompt{
			Messages: []ai.Message{
				ai.UserMessage("Generate information about a person named Alice who is 30 years old and lives in Paris."),
			},
		},
		schema,
		ai.StreamOptions{},
	)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Raw, "expected raw JSON in response")

	var person testPerson
	err = json.Unmarshal([]byte(resp.Raw), &person)
	require.NoError(t, err, "raw JSON should unmarshal to testPerson")

	assert.Equal(t, "Alice", person.Name)
	assert.Equal(t, 30, person.Age)
	assert.Equal(t, "Paris", person.City)

	assert.NotZero(t, resp.Usage.Total, "expected non-zero total tokens")
}

func TestToolCall(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	stream := p.StreamText(
		context.Background(),
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant. Use the get_weather tool to answer weather questions.",
			Messages: []ai.Message{
				ai.UserMessage("What is the weather in Paris?"),
			},
			Tools: []ai.ToolInfo{
				{
					Name:        "get_weather",
					Description: "Get the current weather for a city",
					InputSchema: weatherToolSchema,
				},
			},
		},
		ai.StreamOptions{},
	)

	var (
		gotToolStart bool
		gotToolDelta bool
		gotToolEnd   bool
		finalMsg     *ai.Message
	)

	for e, err := range stream.Events() {
		require.NoError(t, err)
		switch e.Type {
		case ai.EventToolStart:
			gotToolStart = true
		case ai.EventToolDelta:
			gotToolDelta = true
		case ai.EventToolEnd:
			gotToolEnd = true
			require.NotNil(t, e.ToolCall, "tool end event should have ToolCall")
			assert.Equal(t, "get_weather", e.ToolCall.Name)
			assert.NotEmpty(t, e.ToolCall.ID, "tool call should have an ID")
			assert.Contains(t, e.ToolCall.Arguments, "location")
		case ai.EventDone:
			finalMsg = e.Message
		}
	}

	assert.True(t, gotToolStart, "expected tool start event")
	assert.True(t, gotToolDelta, "expected tool delta event")
	assert.True(t, gotToolEnd, "expected tool end event")

	require.NotNil(t, finalMsg, "expected final message")
	assert.Equal(t, ai.StopReasonToolUse, finalMsg.StopReason)

	var foundToolCall bool
	for _, c := range finalMsg.Content {
		if tc, ok := ai.AsContent[ai.ToolCall](c); ok {
			foundToolCall = true
			assert.Equal(t, "get_weather", tc.Name)
			assert.NotEmpty(t, tc.ID)
		}
	}
	assert.True(t, foundToolCall, "final message should contain a ToolCall content block")
}

func TestToolCallMultiTurn(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	tools := []ai.ToolInfo{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a city",
			InputSchema: weatherToolSchema,
		},
	}

	// First turn: model should call the tool.
	firstMsg, err := p.StreamText(
		context.Background(),
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant. Always use the get_weather tool to answer weather questions.",
			Messages: []ai.Message{
				ai.UserMessage("What is the weather in Paris?"),
			},
			Tools: tools,
		},
		ai.StreamOptions{},
	).Result()
	require.NoError(t, err)
	require.Equal(t, ai.StopReasonToolUse, firstMsg.StopReason)

	var toolCall *ai.ToolCall
	for _, c := range firstMsg.Content {
		if tc, ok := ai.AsContent[ai.ToolCall](c); ok {
			toolCall = &tc
			break
		}
	}
	require.NotNil(t, toolCall, "first turn should produce a tool call")

	// Second turn: provide tool result and get final text.
	secondMsg, err := p.StreamText(
		context.Background(),
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant. Always use the get_weather tool to answer weather questions.",
			Messages: []ai.Message{
				ai.UserMessage("What is the weather in Paris?"),
				ai.AssistantMessage(firstMsg.Content...),
				ai.ToolResultMessage(
					toolCall.ID,
					toolCall.Name,
					ai.Text{Text: `{"temperature": "22°C", "condition": "sunny"}`},
				),
			},
			Tools: tools,
		},
		ai.StreamOptions{},
	).Result()
	require.NoError(t, err)
	assert.Equal(t, ai.StopReasonStop, secondMsg.StopReason)

	var text strings.Builder
	for _, c := range secondMsg.Content {
		if tc, ok := ai.AsContent[ai.Text](c); ok {
			text.WriteString(tc.Text)
		}
	}
	assert.NotEmpty(t, text.String(), "second turn should produce text response")
}

func TestToolChoiceRequired(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	msg, err := p.StreamText(
		context.Background(),
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant.",
			Messages: []ai.Message{
				ai.UserMessage("Hello, how are you?"),
			},
			Tools: []ai.ToolInfo{
				{
					Name:        "get_weather",
					Description: "Get the current weather for a city",
					InputSchema: weatherToolSchema,
				},
			},
		},
		ai.StreamOptions{
			ToolChoice: ai.ToolChoiceRequired,
		},
	).Result()
	require.NoError(t, err)
	assert.Equal(t, ai.StopReasonToolUse, msg.StopReason)

	var foundToolCall bool
	for _, c := range msg.Content {
		if _, ok := ai.AsContent[ai.ToolCall](c); ok {
			foundToolCall = true
			break
		}
	}
	assert.True(t, foundToolCall, "ToolChoiceRequired should force a tool call")
}

func TestStreamEventSequence(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	stream := p.StreamText(
		context.Background(),
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant. Be concise.",
			Messages: []ai.Message{
				ai.UserMessage("Say hello"),
			},
		},
		ai.StreamOptions{},
	)

	var eventTypes []ai.EventType
	for e, err := range stream.Events() {
		require.NoError(t, err)
		eventTypes = append(eventTypes, e.Type)
	}

	require.NotEmpty(t, eventTypes, "should have received events")

	// Verify TextStart comes before any TextDelta.
	var firstStart, firstDelta int
	for i, et := range eventTypes {
		if et == ai.EventTextStart {
			firstStart = i
			break
		}
	}
	for i, et := range eventTypes {
		if et == ai.EventTextDelta {
			firstDelta = i
			break
		}
	}
	assert.Less(t, firstStart, firstDelta, "TextStart should precede TextDelta")

	// Verify Done is last event.
	assert.Equal(t, ai.EventDone, eventTypes[len(eventTypes)-1], "last event should be Done")
}

func TestUsageTokens(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	msg, err := p.StreamText(
		context.Background(),
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant.",
			Messages: []ai.Message{
				ai.UserMessage("What is 2+2?"),
			},
		},
		ai.StreamOptions{},
	).Result()
	require.NoError(t, err)

	assert.NotZero(t, msg.Usage.Input, "expected non-zero input tokens")
	assert.NotZero(t, msg.Usage.Output, "expected non-zero output tokens")
	assert.Equal(
		t,
		msg.Usage.Input+msg.Usage.Output,
		msg.Usage.Total,
		"total should equal input + output",
	)
}

func TestThinking(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	thinkingModel := ai.Model{
		ID:       testModelID,
		Name:     "Claude Sonnet",
		API:      "anthropic-messages",
		Provider: "anthropic",
	}

	stream := p.StreamText(
		context.Background(),
		thinkingModel,
		ai.Prompt{
			System: "You are a helpful assistant.",
			Messages: []ai.Message{
				ai.UserMessage("What is the square root of 144?"),
			},
		},
		ai.StreamOptions{
			ThinkingLevel: ai.ThinkingLow,
		},
	)

	var (
		gotThinkStart bool
		gotThinkDelta bool
		gotThinkEnd   bool
		gotTextDelta  bool
		finalMsg      *ai.Message
	)

	for e, err := range stream.Events() {
		require.NoError(t, err)
		switch e.Type {
		case ai.EventThinkStart:
			gotThinkStart = true
		case ai.EventThinkDelta:
			gotThinkDelta = true
		case ai.EventThinkEnd:
			gotThinkEnd = true
		case ai.EventTextDelta:
			gotTextDelta = true
		case ai.EventDone:
			finalMsg = e.Message
		}
	}

	assert.True(t, gotThinkStart, "expected thinking start event")
	assert.True(t, gotThinkDelta, "expected thinking delta event")
	assert.True(t, gotThinkEnd, "expected thinking end event")
	assert.True(t, gotTextDelta, "expected text delta event")

	require.NotNil(t, finalMsg, "expected final message")

	var foundThinking bool
	for _, c := range finalMsg.Content {
		if th, ok := ai.AsContent[ai.Thinking](c); ok {
			foundThinking = true
			assert.NotEmpty(t, th.Thinking, "thinking content should not be empty")
			assert.NotEmpty(t, th.Signature, "thinking signature should not be empty")
		}
	}
	assert.True(t, foundThinking, "final message should contain a Thinking content block")
}

func TestContextCancellation(t *testing.T) {
	p, cleanup := newTestProvider(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	stream := p.StreamText(
		ctx,
		testModel,
		ai.Prompt{
			System: "You are a helpful assistant.",
			Messages: []ai.Message{
				ai.UserMessage("Write a very long essay about the history of computing."),
			},
		},
		ai.StreamOptions{},
	)

	_, err := stream.Result()
	assert.Error(t, err, "cancelled context should produce an error")
}
