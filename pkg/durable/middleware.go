package durable

import (
	"context"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/session"
)

// Runner executes one durable run. It is the shape [Agent.Run] has, so
// middleware can wrap a run without knowing anything else about the
// agent.
type Runner func(ctx context.Context, entries ...session.Entry) *Stream

// Middleware wraps a [Runner] — the http.Handler idiom, per run rather
// than per request. It can add entries to a run, observe the stream it
// produces, or refuse the run outright.
//
// The chain is instantiated once per agent instance, so state belongs
// in the closure the middleware creates when it is applied:
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
// [Agent.Fork] re-instantiates the chain for the child, so a fork gets
// its own closures rather than sharing the parent's — one session's
// per-run state can never leak into another's.
//
// Middleware that decides a run must not happen — a permission gate, a
// quota — returns [Fail] instead of calling next, and the caller sees an
// ordinary failed stream.
//
// Middleware sees a whole run. Interception inside a run — per LLM call,
// per tool — is [agent.Hook], which the durable agent forwards to the
// inner loop untouched.
type Middleware func(next Runner) Runner

// WithMiddleware registers per-run middleware, applied around
// [Agent.Run] in registration order — the first registered is the
// outermost. Repeated calls append.
func WithMiddleware(mw ...Middleware) agent.Option {
	return mutate(func(e *ext) { e.middleware = append(e.middleware, mw...) })
}

// chain wraps base with mw, applying it so the first-registered
// middleware ends up outermost.
func chain(mw []Middleware, base Runner) Runner {
	for i := len(mw) - 1; i >= 0; i-- {
		base = mw[i](base)
	}
	return base
}

// middlewareFrom reads the registered middleware back out of an option
// list. [Agent.Fork] uses it to build the child's own chain: the
// Middleware funcs are re-invoked, so the child's closures are fresh.
func middlewareFrom(opts []agent.Option) []Middleware {
	return slot.From(agent.ApplyOptions(opts...)).middleware
}
