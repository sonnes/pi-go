package anthropic

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// cacheMarker resolves the stream options' cache retention into an Anthropic
// cache_control value. The second return value is false when caching is
// disabled (either explicitly via [ai.CacheRetentionNone]) and callers should
// skip marker placement entirely.
//
// The 1h TTL is only attached when the client talks to api.anthropic.com
// directly. Proxies and compatible endpoints receive the marker without a
// TTL so the request still serializes cleanly.
func cacheMarker(
	opts ai.StreamOptions,
	baseURL string,
) (anthropic.CacheControlEphemeralParam, bool) {
	retention := ai.ResolveCacheRetention(opts.CacheRetention)
	if retention == ai.CacheRetentionNone {
		return anthropic.CacheControlEphemeralParam{}, false
	}

	marker := anthropic.NewCacheControlEphemeralParam()
	if retention == ai.CacheRetentionLong && isOfficialAnthropicURL(baseURL) {
		marker.TTL = anthropic.CacheControlEphemeralTTLTTL1h
	}
	return marker, true
}

// isOfficialAnthropicURL reports whether the configured base URL supports the
// 1h cache TTL extension. An empty URL is treated as official since the SDK's
// default routes to api.anthropic.com. OpenRouter is included because it
// explicitly documents support for the "ttl": "1h" field.
func isOfficialAnthropicURL(baseURL string) bool {
	if baseURL == "" {
		return true
	}
	return strings.Contains(baseURL, "api.anthropic.com") ||
		strings.Contains(baseURL, "openrouter.ai")
}

// applyCacheControlToLastBlock attaches a cache_control marker to the final
// content block of the final message. This is the terminal-breakpoint
// placement strategy: on each turn a marker says "everything before this
// point is cacheable," and on the next turn the previous terminal block
// falls into the cached interior automatically.
func applyCacheControlToLastBlock(
	messages []anthropic.MessageParam,
	cc anthropic.CacheControlEphemeralParam,
) {
	if len(messages) == 0 {
		return
	}
	last := &messages[len(messages)-1]
	if len(last.Content) == 0 {
		return
	}
	block := &last.Content[len(last.Content)-1]
	switch {
	case block.OfText != nil:
		block.OfText.CacheControl = cc
	case block.OfToolResult != nil:
		block.OfToolResult.CacheControl = cc
	case block.OfToolUse != nil:
		block.OfToolUse.CacheControl = cc
	case block.OfImage != nil:
		block.OfImage.CacheControl = cc
	case block.OfDocument != nil:
		block.OfDocument.CacheControl = cc
	}
}
