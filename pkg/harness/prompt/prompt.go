package prompt

import (
	"context"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/session"
)

// Env is the snapshot one agent build hands to its builders: everything
// resolved for that build, and nothing derived from it. It is defined
// here and populated by the harness — the http.Request pattern, where
// the consumer owns the type and the runtime fills it in.
//
// A fresh Env is assembled per build, so a builder sees the artifacts
// as they were at that moment and the resulting prompt is stable for
// the built agent's lifetime.
type Env struct {
	// Model is the resolved model this build will run on.
	Model ai.Model

	// Tools describes the tools the build will expose, synthesized
	// ones included.
	Tools []ai.ToolInfo

	// Agents are the resolved agent definitions, in order. The harness
	// synthesizes no tool from them — like a directory-bound
	// instruction document, they are resolved and handed over, and what
	// a builder or product makes of them is its decision. [Default]
	// renders nothing for them.
	Agents []def.Agent

	// Skills are the resolved skills, bodies included. [Default] renders
	// only their names and descriptions: a body reaches the model
	// through the skill tool, not through the prompt.
	Skills []def.Skill

	// Instructions are the resolved instruction documents, in resolver
	// order. [Default] renders the ones with an empty
	// [def.Instructions.Dir] and leaves the rest here for a builder that
	// knows when a directory-bound document should apply.
	Instructions []def.Instructions

	// WorkDir is the directory the agent works in. It is the only
	// environment fact the harness supplies — platform, date, and git
	// state are the [Seeder]'s business.
	WorkDir string
}

// Builder builds the system prompt for one agent build. It runs on
// every build, and its result is the built agent's identity for its
// whole lifetime.
type Builder func(ctx context.Context, env *Env) (string, error)

// Seeder builds the entries a fresh session starts with — the
// environment block, memory, anything true at the start of a
// conversation and worth keeping in it.
//
// The harness prepends them to the first run of a fresh session as meta
// entries: the model reads them, the transcript hides them, and a
// resumed session skips them because they are already in its history.
// Context that is only true right now belongs in middleware as an
// ephemeral entry, not here.
type Seeder func(ctx context.Context, env *Env) ([]session.Entry, error)
