package google_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ai "github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/google"
)

// A base URL sends the module's traffic somewhere other than Google's own
// endpoint — a gateway, a proxy, a compatible API. It is the one thing the
// other provider modules already accept and this one did not.
//
// The version path is not part of it: genai appends its own ("v1beta"), so
// the option takes a host and nothing more.
func TestWithBaseURL(t *testing.T) {
	var got string

	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			got = r.URL.String()

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewBufferString("")),
				Request:    r,
			}, nil
		}),
	}

	p, err := google.New(
		google.WithAPIKey("fake-key"),
		google.WithBaseURL("https://gw.example"),
		google.WithHTTPClient(client),
	)
	require.NoError(t, err)

	_, _ = p.StreamText(context.Background(), testModelDef(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("hi")},
	}, ai.StreamOptions{}).Wait()

	require.NotEmpty(t, got, "the transport saw no request")
	assert.True(
		t,
		strings.HasPrefix(got, "https://gw.example/"),
		"traffic went to %q, not the configured base URL",
		got,
	)
}

// An unset base URL leaves the module on Google's own endpoint.
func TestWithoutBaseURL(t *testing.T) {
	var got string

	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			got = r.URL.String()

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewBufferString("")),
				Request:    r,
			}, nil
		}),
	}

	p, err := google.New(
		google.WithAPIKey("fake-key"),
		google.WithHTTPClient(client),
	)
	require.NoError(t, err)

	_, _ = p.StreamText(context.Background(), testModelDef(), ai.Prompt{
		Messages: []ai.Message{ai.UserMessage("hi")},
	}, ai.StreamOptions{}).Wait()

	require.NotEmpty(t, got, "the transport saw no request")
	assert.True(
		t,
		strings.HasPrefix(got, "https://generativelanguage.googleapis.com/"),
		"traffic went to %q, not the default endpoint",
		got,
	)
}
