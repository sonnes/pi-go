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
// Harness is a factory and not an agent, because two things need a
// factory. One configuration serves many sessions. To seed a fresh
// session, the harness must know at creation time that the session is
// fresh. A Harness holds configuration only — no session and no
// conversation — so it is safe to share across sessions and goroutines.
//
// The configuration it holds is a baseline. [Harness.Agent] accepts the
// same options and overlays them for each build. One harness can
// therefore serve many working directories.
type Harness struct {
	base     *ext
	baseline *build
	opts     []agent.Option
}

// build is the merged and compiled configuration for one agent build:
// the baseline with the overlay of that build applied. compile resolves
// the model and indexes the toolbox. Everything after [Harness.Agent] —
// resolution and prompt building — reads a build, never the harness.
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

// New makes sure that the harness configuration is usable, then returns
// the factory.
//
// The option list is flat and mixes all three layers. Harness options
// configure the factory. The harness keeps everything else — [durable]
// and [agent] options alike — and forwards it to each agent it mints.
//
// New examines the baseline the way a build does. The catalog must be
// present, the default model must resolve, and no tool can claim a
// reserved name. The harness examines the artifacts themselves on each
// build, because the resolvers run fresh every time.
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
// caller. The harness compiles the system prompt from the artifacts,
// and the compiled prompt overwrites the prompt of the caller without
// warning. The harness reports this collision as an error instead.
var errCallerSystemPrompt = errors.New(
	"harness: the harness builds the system prompt; use WithPromptBuilder",
)

// compile turns a merged [ext] into a usable [build]. It resolves the
// model, indexes the tools, and fills in the defaults the caller left
// out.
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

	// compile wires the defaults here, not in the option, so a caller
	// who overrides one default still gets the other.
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

// overlay compiles the baseline together with the per-build options of
// the caller. A build that overlays nothing reuses the baseline, so the
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
