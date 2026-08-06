package harness

import (
	"errors"
	"fmt"
	"os"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
)

// Harness is the factory that compiles declarative artifacts into
// running agents. Create one with [New] and mint agents from it with
// [Harness.Agent].
//
// It is a factory rather than an agent because two things need one:
// one configuration serves many sessions, and seeding a fresh session
// requires knowing at creation time whether it is fresh. A Harness
// holds configuration only — no session, no conversation — so it is
// safe to share across sessions and goroutines.
//
// The configuration it holds is a baseline. [Harness.Agent] accepts the
// same options and overlays them per build, which is what lets one
// harness serve many working directories.
type Harness struct {
	base     *ext
	baseline *build
	opts     []agent.Option
}

// build is one agent build's merged and compiled configuration: the
// baseline with that build's overlay applied, with the model resolved
// and the toolbox indexed. Everything downstream of [Harness.Agent] —
// resolution, prompt building — reads a build, never the harness.
type build struct {
	lm      ai.LanguageModel
	workDir string

	agents       []def.AgentResolver
	skills       []def.SkillResolver
	instructions []def.InstructionResolver

	tools   toolbox
	builder prompt.Builder
	seeder  prompt.Seeder
}

// New validates the harness configuration and returns the factory.
//
// The option list is flat and mixes all three layers: harness options
// configure the factory, and everything else — [durable] and [agent]
// options alike — is remembered and forwarded to each agent this
// harness mints.
//
// New checks the baseline the way a build would: the catalog must be
// present, the default model must resolve, and no tool may claim a
// reserved name. Everything about the artifacts themselves is checked
// per build, since resolvers run fresh every time.
func New(opts ...agent.Option) (*Harness, error) {
	cfg := agent.ApplyOptions(opts...)
	if cfg.SystemPrompt != "" {
		return nil, errCallerSystemPrompt
	}
	base := extOf(cfg)

	baseline, err := compile(base)
	if err != nil {
		return nil, err
	}

	return &Harness{
		base:     base,
		baseline: baseline,
		opts:     opts,
	}, nil
}

// errCallerSystemPrompt rejects [agent.WithSystemPrompt] from the
// caller. The harness compiles the system prompt from the artifacts and
// would silently clobber whatever the caller set, so the collision is
// an error instead.
var errCallerSystemPrompt = errors.New(
	"harness: the harness builds the system prompt; use WithPromptBuilder",
)

// compile turns a merged [ext] into a usable [build]: resolve the model,
// index the tools, fill in the defaults the caller left out.
func compile(e *ext) (*build, error) {
	if e.cat == nil {
		return nil, errors.New("harness: catalog is required; pass WithCatalog")
	}
	if e.defaultModel == "" {
		return nil, errors.New("harness: default model is required; pass WithDefaultModel")
	}
	lm, err := e.cat.LanguageModel(e.defaultModel)
	if err != nil {
		return nil, fmt.Errorf("harness: default model: %w", err)
	}

	box, err := newToolbox(e.tools)
	if err != nil {
		return nil, err
	}

	workDir := e.workDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// The defaults are wired here, not in the option, so that a caller
	// who overrides one still gets the other.
	builder := e.builder
	if builder == nil {
		builder = prompt.Default
	}
	seeder := e.seeder
	if seeder == nil {
		seeder = prompt.DefaultSeed
	}

	return &build{
		lm:           lm,
		workDir:      workDir,
		agents:       e.agents,
		skills:       e.skills,
		instructions: e.instructions,
		tools:        box,
		builder:      builder,
		seeder:       seeder,
	}, nil
}

// overlay compiles the baseline with the caller's per-build options
// merged in. A build that overlays nothing reuses the baseline, so the
// common path costs no work.
func (h *Harness) overlay(opts []agent.Option) (*build, error) {
	if len(opts) == 0 {
		return h.baseline, nil
	}
	cfg := agent.ApplyOptions(opts...)
	if cfg.SystemPrompt != "" {
		return nil, errCallerSystemPrompt
	}
	over := extOf(cfg)
	if over.isZero() {
		return h.baseline, nil
	}
	return compile(h.base.merge(over))
}
