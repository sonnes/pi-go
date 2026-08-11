package openai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	aiopenai "github.com/sonnes/pi-go/pkg/ai/provider/openai"
)

// sseServer creates a test server that returns SSE chunks for the Chat
// Completions endpoint at /v1/chat/completions.
func sseServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func newMockProvider(t *testing.T, chunks []string) (*aiopenai.Provider, func()) {
	t.Helper()
	srv := sseServer(t, chunks)
	p := aiopenai.New(
		option.WithAPIKey("fake-key"),
		option.WithBaseURL(srv.URL+"/v1"),
	)
	return p, srv.Close
}

func TestMock_UsageCost(t *testing.T) {
	chunks := []string{
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500,"prompt_tokens_details":{"cached_tokens":200}}}`,
	}

	p, cleanup := newMockProvider(t, chunks)
	defer cleanup()

	model := testModel()
	model.Cost = ai.Cost{Input: 2, Output: 8, CacheRead: 0.5}

	msg, err := p.StreamText(context.Background(), model, ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("hi")},
	}, ai.StreamOptions{}).Wait()

	require.NoError(t, err)
	require.NotNil(t, msg)

	// On the wire, prompt_tokens includes cached_tokens. Usage.Input reports
	// only the uncached remainder, so the two never bill twice.
	assert.Equal(t, 800, msg.Usage.Input)
	assert.Equal(t, 200, msg.Usage.CacheRead)

	cost := msg.Usage.Cost
	assert.InDelta(t, 0.0016, cost.Input, 1e-9)
	assert.InDelta(t, 0.004, cost.Output, 1e-9)
	assert.InDelta(t, 0.0001, cost.CacheRead, 1e-9)
}
