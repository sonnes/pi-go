package durable

import (
	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Factory builds a fresh inner agent loop for one run. [New] takes one,
// and the durable agent calls it once per run. It closes what the
// factory returns when the run ends.
//
// The options a Factory receives are the base options of the agent plus
// the rehydrated history of that run. A factory therefore decides only
// which loop implementation runs. It does not decide what that loop is
// configured with.
//
// A Factory takes no context. A loop that owns a subprocess starts it
// lazily on the first [agent.Agent.Run], against the context of that
// run.
//
// [Model] is the Factory for an ordinary API-backed session. A
// subprocess CLI, for example the Claude Code agent, supplies its own.
type Factory func(opts ...agent.Option) (agent.Agent, error)

// Model returns the [Factory] that builds the default in-process loop
// over lm. It is what an API-backed session uses:
//
//	da, err := durable.New(ctx, durable.Model(lm), durable.WithSessionID(id))
func Model(lm ai.LanguageModel) Factory {
	return func(opts ...agent.Option) (agent.Agent, error) {
		return agent.New(lm, opts...), nil
	}
}
