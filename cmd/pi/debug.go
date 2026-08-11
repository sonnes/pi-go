package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// PI_DEBUG_HTTP enables verbose HTTP logging when it holds a non-empty
// value. The logger writes the status and the body of every non-2xx response
// to stderr. It keeps the body available to downstream parsers. The log helps
// with opaque errors, such as the `400 Bad Request` of the Codex backend. The
// upstream SDK truncates the body of that error.
const debugHTTPEnvVar = "PI_DEBUG_HTTP"

// maybeDebugTransport wraps base in a verbose logger when PI_DEBUG_HTTP holds
// a value. If the variable is empty, it returns base unchanged. A nil base is
// safe. In that case the function uses [http.DefaultTransport].
func maybeDebugTransport(base http.RoundTripper) http.RoundTripper {
	if os.Getenv(debugHTTPEnvVar) == "" {
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &debugRoundTripper{base: base}
}

// debugRoundTripper logs HTTP errors and non-2xx responses to stderr.
type debugRoundTripper struct {
	base http.RoundTripper
}

func (d *debugRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := d.base.RoundTrip(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[http %s %s] transport error: %v\n", req.Method, req.URL, err)
		return resp, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			fmt.Fprintf(os.Stderr,
				"[http %s %s] %d (body read failed: %v)\n",
				req.Method, req.URL, resp.StatusCode, readErr,
			)
		} else {
			fmt.Fprintf(os.Stderr,
				"[http %s %s] %d\n  headers: %s\n  body: %s\n",
				req.Method, req.URL, resp.StatusCode,
				summarizeHeaders(resp.Header), string(body),
			)
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, nil
}

// summarizeHeaders returns a compact one-line header summary for the debug
// log. It skips the noisy headers and the sensitive headers.
func summarizeHeaders(h http.Header) string {
	skip := map[string]bool{
		"Set-Cookie":                true,
		"Cf-Ray":                    true,
		"Cf-Cache-Status":           true,
		"X-Request-Id":              true,
		"Server":                    true,
		"Strict-Transport-Security": true,
		"Alt-Svc":                   true,
	}
	parts := make([]string, 0, len(h))
	for k, v := range h {
		if skip[k] || len(v) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, v[0]))
	}
	return strings.Join(parts, " ")
}
