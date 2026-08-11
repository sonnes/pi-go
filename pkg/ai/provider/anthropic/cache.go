package anthropic

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	ai "github.com/sonnes/pi-go/pkg/ai"
)

// cacheMarker converts the cache retention in the stream options into an
// Anthropic cache_control value. The second return value is false when
// [ai.CacheRetentionNone] turns caching off. The caller must then place no
// marker.
//
// The function attaches the 1h TTL only when the client talks to
// api.anthropic.com directly. A proxy or a compatible endpoint gets the marker
// without a TTL, so the request still serializes correctly.
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

// isOfficialAnthropicURL reports whether the base URL supports the 1h cache TTL
// extension. An empty URL counts as official, because the SDK default routes to
// api.anthropic.com. OpenRouter also counts as official, because it documents
// support for the "ttl": "1h" field.
func isOfficialAnthropicURL(baseURL string) bool {
	if baseURL == "" {
		return true
	}
	return strings.Contains(baseURL, "api.anthropic.com") ||
		strings.Contains(baseURL, "openrouter.ai")
}

// applyCacheControlToLastBlock attaches a cache_control marker to the final
// content block of the final message. This is the terminal-breakpoint
// strategy. On each turn, the marker states that all content before that point
// is cacheable. On the next turn, the previous terminal block moves into the
// cached interior.
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
