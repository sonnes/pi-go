package prompt

import (
	"context"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/session"
)

// Env is the snapshot one agent build hands to its builders: everything
// resolved for that build, and nothing derived from it. This package
// defines Env and the harness fills it in. That is the http.Request
// pattern, where the consumer owns the type and the runtime fills it in.
//
// Each build assembles a fresh Env. A builder therefore sees the
// artifacts as they were at that moment, and the prompt it returns is
// stable for the lifetime of the built agent.
type Env struct {
	// Model is the resolved model this build will run on.
	Model ai.Model

	// Tools describes the tools the build will expose, synthesized
	// ones included.
	Tools []ai.ToolInfo

	// Agents are the resolved agent definitions, in order. The harness
	// synthesizes no tool from them. Like a directory-bound instruction
	// document, the harness resolves them and hands them over. A builder
	// or product decides what to make of them. [Default] writes nothing
	// for them.
	Agents []def.Agent

	// Skills are the resolved skills, bodies included. [Default] writes
	// only the names and descriptions. A body reaches the model through
	// the skill tool, not through the prompt.
	Skills []def.Skill

	// Instructions are the resolved instruction documents, in resolver
	// order. [Default] writes the ones with an empty
	// [def.Instructions.Dir] into the prompt. It leaves the rest here for
	// a builder that knows when a directory-bound document applies.
	Instructions []def.Instructions

	// WorkDir is the directory the agent works in. It is the only
	// environment fact the harness supplies. Platform, date, and git
	// state belong to the [Seeder].
	WorkDir string
}

// Builder builds the system prompt for one agent build. It runs on every
// build. Its result is the identity of the built agent for the whole
// lifetime of that agent.
type Builder func(ctx context.Context, env *Env) (string, error)

// Seeder builds the entries a fresh session starts with — the
// environment block, memory, anything true at the start of a
// conversation and worth keeping in it.
//
// The harness prepends them to the first run of a fresh session as meta
// entries. The model reads them and the transcript hides them. A resumed
// session skips them, because they are already in its history. Context
// that is only true right now belongs in middleware as an ephemeral
// entry, not here.
type Seeder func(ctx context.Context, env *Env) ([]session.Entry, error)
