package durable

import (
	"context"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/session"
)

// Runner runs one durable run. It has the shape of [Agent.Run], so
// middleware can wrap a run without any other knowledge of the agent.
type Runner func(ctx context.Context, entries ...session.Entry) *Stream

// Middleware wraps a [Runner]. It is the http.Handler idiom, per run
// rather than per request. A middleware can add entries to a run,
// observe the stream that the run produces, or refuse the run.
//
// The package instantiates the chain once per agent instance. State
// therefore belongs in the closure that the middleware creates when the
// package applies it:
//
//	func once(entry session.Entry) durable.Middleware {
//	    return func(next durable.Runner) durable.Runner {
//	        done := false // per-agent, not per-process
//	        return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
//	            if !done {
//	                done, entries = true, append([]session.Entry{entry}, entries...)
//	            }
//	            return next(ctx, entries...)
//	        }
//	    }
//	}
//
// [Agent.Fork] instantiates the chain again for the child. A fork
// therefore gets its own closures, and it does not share the closures
// of the parent. The per-run state of one session can never leak into
// another session.
//
// A middleware that decides a run must not happen, such as a permission
// gate or a quota, returns [Fail] instead of a call to next. The caller
// then sees an ordinary failed stream.
//
// Middleware sees a whole run. Interception inside a run, per LLM call
// or per tool, is [agent.Hook]. The durable agent forwards a hook to
// the inner loop untouched.
type Middleware func(next Runner) Runner

// WithMiddleware registers per-run middleware. The package applies the
// middleware around [Agent.Run] in registration order, and the first
// registered is the outermost. Repeated calls append.
func WithMiddleware(mw ...Middleware) agent.Option {
	return mutate(func(e *ext) { e.middleware = append(e.middleware, mw...) })
}

// chain wraps base with mw. It applies the middleware so that the
// first-registered one is the outermost.
func chain(mw []Middleware, base Runner) Runner {
	for i := len(mw) - 1; i >= 0; i-- {
		base = mw[i](base)
	}
	return base
}

// middlewareFrom reads the registered middleware back out of an option
// list. [Agent.Fork] uses it to build the chain of the child. The
// Middleware funcs run again, so the closures of the child are fresh.
func middlewareFrom(opts []agent.Option) []Middleware {
	return slot.From(agent.ApplyOptions(opts...)).middleware
}
