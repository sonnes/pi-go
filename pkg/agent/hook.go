package agent

import (
	"context"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Middleware wraps tool execution. It receives the [ai.ToolCall] and
// a [ToolRunner] that executes the tool. The middleware controls whether
// the tool runs by calling or skipping next.
//
// Middleware must be safe for concurrent use when tools run in parallel.
type Middleware func(ctx context.Context, call ai.ToolCall, next ToolRunner) (ai.ToolResult, error)

// ToolRunner executes a tool call. Call it to proceed; skip it to block.
type ToolRunner func(ctx context.Context) (ai.ToolResult, error)

// Chain composes middleware left-to-right. The first middleware is the
// outermost wrapper.
func Chain(mw ...Middleware) Middleware {
	switch len(mw) {
	case 0:
		return nil
	case 1:
		return mw[0]
	}
	return func(
		ctx context.Context,
		call ai.ToolCall,
		next ToolRunner,
	) (ai.ToolResult, error) {
		runner := next
		for i := len(mw) - 1; i >= 0; i-- {
			current := mw[i]
			inner := runner
			runner = func(ctx context.Context) (ai.ToolResult, error) {
				return current(ctx, call, inner)
			}
		}
		return runner(ctx)
	}
}
