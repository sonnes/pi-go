package google_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/google"
)

// roundTripperFunc adapts a function to [http.RoundTripper].
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// newMockProvider returns a provider whose transport answers every request
// with body, so streaming tests need no cassette.
func newMockProvider(t *testing.T, body string) *google.Provider {
	t.Helper()

	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Request:    r,
			}, nil
		}),
	}

	p, err := google.New(
		google.WithAPIKey("fake-key"),
		google.WithHTTPClient(client),
	)
	require.NoError(t, err)
	return p
}

func TestMock_UsageCost(t *testing.T) {
	body := `data: {"candidates":[{"content":{"parts":[{"text":"Hi"}],"role":"model"},` +
		`"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":1000,` +
		`"candidatesTokenCount":500,"totalTokenCount":1500,"cachedContentTokenCount":200},` +
		`"modelVersion":"gemini-2.5-flash"}` + "\n\n"

	p := newMockProvider(t, body)

	model := testModelDef()
	model.Cost = ai.Cost{Input: 2, Output: 8, CacheRead: 0.5}

	msg, err := p.StreamText(context.Background(), model, ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("hi")},
	}, ai.StreamOptions{}).Wait()

	require.NoError(t, err)
	require.NotNil(t, msg)

	// promptTokenCount includes cachedContentTokenCount on the wire;
	// Usage.Input reports only the uncached remainder so the two never bill
	// twice.
	assert.Equal(t, 800, msg.Usage.Input)
	assert.Equal(t, 200, msg.Usage.CacheRead)

	cost := msg.Usage.Cost
	assert.InDelta(t, 0.0016, cost.Input, 1e-9)
	assert.InDelta(t, 0.004, cost.Output, 1e-9)
	assert.InDelta(t, 0.0001, cost.CacheRead, 1e-9)
}
