package harness

import (
	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
)

// slot is the [agent.Config] extension slot the harness reads its
// options from. Every harness With* option is an [agent.Option] that
// layers onto one ext value. One flat option list therefore configures
// the harness, the durable session, and the inner loop at once. The
// harness takes its own slot and forwards the rest down.
//
// The slot holds an ext by value. An option list with no harness
// options therefore reads back as the zero ext, and not as nothing at
// all.
var slot = agent.Slot[ext]{Key: "harness"}

// ext accumulates harness configuration from the With* options.
type ext struct {
	cat          *catalog.Catalog
	defaultModel string
	workDir      string

	agents       []def.AgentResolver
	skills       []def.SkillResolver
	instructions []def.InstructionResolver

	tools   []ai.Tool
	builder prompt.Builder
	seeder  prompt.Seeder
}

// extOf reads the harness slot out of an applied configuration and
// folds in the tools registered through [agent.WithTools]. The option
// list is flat, so either spelling can carry tools. Both feed one
// toolbox, and neither one replaces the other without warning.
func extOf(cfg agent.Config) *ext {
	e := slot.From(cfg)
	if len(cfg.Tools) > 0 {
		e.tools = concat(e.tools, cfg.Tools)
	}
	return &e
}

// isZero reports whether an ext carries no configuration at all. An
// empty ext is the signal that a build can reuse the compiled baseline.
// The build does not have to merge and compile again.
func (e *ext) isZero() bool {
	return e.cat == nil &&
		e.defaultModel == "" &&
		e.workDir == "" &&
		e.builder == nil &&
		e.seeder == nil &&
		len(e.tools) == 0 &&
		len(e.agents) == 0 &&
		len(e.skills) == 0 &&
		len(e.instructions) == 0
}

// merge overlays a per-build ext onto a baseline ext and returns a new
// value. It touches neither input, so one build can never disturb the
// harness or another build.
//
// Scalars override: a per-build working directory, model, catalog, or
// builder replaces the baseline value.
//
// Everything else appends, baseline first. A per-build resolver is an
// additional source, not a replacement. The project directory of a
// session adds resources to what the process already knows. The
// per-build resolver comes last, so it wins a name collision. Tools
// override by name, and they need the same baseline-first order.
func (e *ext) merge(o *ext) *ext {
	out := *e

	if o.cat != nil {
		out.cat = o.cat
	}
	if o.defaultModel != "" {
		out.defaultModel = o.defaultModel
	}
	if o.workDir != "" {
		out.workDir = o.workDir
	}
	if o.builder != nil {
		out.builder = o.builder
	}
	if o.seeder != nil {
		out.seeder = o.seeder
	}

	out.agents = concat(e.agents, o.agents)
	out.skills = concat(e.skills, o.skills)
	out.instructions = concat(e.instructions, o.instructions)
	out.tools = concat(e.tools, o.tools)

	return &out
}

// concat joins two slices into a fresh one, so neither input aliases
// the result.
func concat[S any](a, b []S) []S {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make([]S, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// mutate returns an option that layers f onto the ext in the slot.
func mutate(f func(*ext)) agent.Option {
	return slot.Mutate(func(e ext) ext {
		f(&e)
		return e
	})
}

// WithCatalog sets the registry the harness resolves the default model
// spec through. Required.
func WithCatalog(c *catalog.Catalog) agent.Option {
	return mutate(func(e *ext) { e.cat = c })
}

// WithDefaultModel sets the "<provider>/<model>" spec that every agent
// from this harness runs on. Required. [New] resolves the spec against
// the catalog.
func WithDefaultModel(spec string) agent.Option {
	return mutate(func(e *ext) { e.defaultModel = spec })
}

// WithWorkDir sets the directory the agent works in. The builders read
// it as [prompt.Env.WorkDir]. Without this option, the harness uses the
// working directory of the process.
func WithWorkDir(dir string) agent.Option {
	return mutate(func(e *ext) { e.workDir = dir })
}

// WithTools registers the tools available to the agent. A name must not
// collide with the reserved "skill" and "agent" tool names.
//
// A name registered twice — across calls, across the baseline and build
// boundary, or within one call — is last-wins. The later tool replaces
// the earlier one at the position where the name first appeared. A
// session overrides a baseline tool this way, a sandboxed "read" for
// example, and the list the model sees keeps its order. Artifacts
// override by name in the same way.
func WithTools(tools ...ai.Tool) agent.Option {
	return mutate(func(e *ext) { e.tools = append(e.tools, tools...) })
}

// WithAgents registers resolvers that supply agent definitions, lowest
// source first. Repeated calls append, and so does a call to
// [Harness.Agent]. The project of a session adds resources to what the
// process knows. That call comes last, so it wins a name collision.
//
//	harness.WithAgents(
//	    fs.Agents(builtin, "agents"),
//	    fs.Agents(home, "agents"),
//	)
//
// Definitions from different sources combine. Two definitions that
// claim the same name are the same agent declared twice, and the
// highest source wins. A [def.Agent.Scope] keeps them apart instead. It
// qualifies the name with the directory the definition governs.
//
// The resolved definitions are data, not behavior. They reach the
// prompt builder as [prompt.Env.Agents] and the caller through
// [Harness.Env]. The harness synthesizes no tool from them.
func WithAgents(r ...def.AgentResolver) agent.Option {
	return mutate(func(e *ext) { e.agents = append(e.agents, r...) })
}

// WithSkills registers resolvers that supply skills, lowest source
// first. See [WithAgents] for how sources combine and what
// [def.Skill.Scope] does to a name.
func WithSkills(r ...def.SkillResolver) agent.Option {
	return mutate(func(e *ext) { e.skills = append(e.skills, r...) })
}

// WithInstructions registers resolvers that supply instruction
// documents, lowest source first. Documents have no name to collide on,
// so every resolved document applies. A house document and a project
// document both reach the prompt, in that order.
func WithInstructions(r ...def.InstructionResolver) agent.Option {
	return mutate(func(e *ext) { e.instructions = append(e.instructions, r...) })
}

// WithPromptBuilder overrides the system prompt builder. Without this
// option, the harness uses [prompt.Default].
func WithPromptBuilder(b prompt.Builder) agent.Option {
	return mutate(func(e *ext) { e.builder = b })
}

// WithSeed overrides the first-run entry builder. Without this option,
// or with a nil seeder, the harness uses [prompt.DefaultSeed]. To seed
// nothing at all, pass [prompt.NoSeed].
func WithSeed(s prompt.Seeder) agent.Option {
	return mutate(func(e *ext) { e.seeder = s })
}
