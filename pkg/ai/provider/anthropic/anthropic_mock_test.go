package anthropic_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
)

// sseEvent is one named SSE frame of the Messages API stream.
type sseEvent struct {
	name string
	data string
}

// newMockProvider returns a provider that uses a test server. The server
// replays events, so streaming tests need no cassette.
func newMockProvider(t *testing.T, events []sseEvent) (*anthropic.Provider, func()) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, e.data)
		}
	}))

	p := anthropic.New(
		anthropic.WithAPIKey("fake-key"),
		anthropic.WithBaseURL(srv.URL),
	)
	return p, srv.Close
}

func TestMock_UsageTotalIncludesCacheTokens(t *testing.T) {
	events := []sseEvent{
		{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message",` +
			`"role":"assistant","content":[],"model":"claude-sonnet-4-20250514",` +
			`"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":800,` +
			`"output_tokens":0,"cache_read_input_tokens":200,` +
			`"cache_creation_input_tokens":100}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,` +
			`"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,` +
			`"delta":{"type":"text_delta","text":"Hi"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn",` +
			`"stop_sequence":null},"usage":{"output_tokens":500}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}

	p, cleanup := newMockProvider(t, events)
	defer cleanup()

	model := ai.Model{
		ID:   "claude-sonnet-4-20250514",
		Cost: ai.Cost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
	}

	msg, err := p.StreamText(context.Background(), model, ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("hi")},
	}, ai.StreamOptions{}).Wait()

	require.NoError(t, err)
	require.NotNil(t, msg)

	assert.Equal(t, 800, msg.Usage.Input)
	assert.Equal(t, 500, msg.Usage.Output)
	assert.Equal(t, 200, msg.Usage.CacheRead)
	assert.Equal(t, 100, msg.Usage.CacheWrite)

	// Each of the four token kinds bills once, at its own rate.
	cost := msg.Usage.Cost
	assert.InDelta(t, 0.0024, cost.Input, 1e-9)
	assert.InDelta(t, 0.0075, cost.Output, 1e-9)
	assert.InDelta(t, 0.00006, cost.CacheRead, 1e-9)
	assert.InDelta(t, 0.000375, cost.CacheWrite, 1e-9)
}
