package def

import (
	"context"
)

// AgentResolver discovers agent definitions. It lives here rather than
// in the harness, so a resolver — a filesystem scan, a database query —
// depends on def alone and never on the harness that consumes it.
//
// One resolver is one source. The harness takes a list of them and
// combines what they return. The package documentation describes that
// union.
type AgentResolver interface {
	Agents(ctx context.Context) ([]Agent, error)
}

// SkillResolver discovers skills.
type SkillResolver interface {
	Skills(ctx context.Context) ([]Skill, error)
}

// InstructionResolver discovers instruction documents.
type InstructionResolver interface {
	Instructions(ctx context.Context) ([]Instructions, error)
}

// Agent is a declarative agent definition — an identity, a description,
// and advisory metadata about how it wants to run. The harness resolves,
// qualifies, and combines definitions exactly like skills. It then hands
// them to the prompt builder and to the Env callers of the harness as
// data. The harness synthesizes no tool from them. The v1 "agent" spawn
// tool was removed, and the name stays reserved for its return. A product
// decides what to do with the resolved definitions: route to them, spawn
// them with its own machinery, or list them in a UI.
type Agent struct {
	// Name identifies the agent. It must be unique within a resolver.
	Name string

	// Description tells a reader — human or model — what this agent is
	// for.
	Description string

	// Prompt is the identity of the agent — the body of its system
	// prompt, for whatever runs it.
	Prompt string

	// Model is a catalog spec ("<provider>/<model>") the definition
	// asks for. This is advisory metadata. The harness does not resolve
	// it and does not make sure that it is valid. The product that
	// consumes the definition does that.
	Model string

	// Tools names the tools the definition asks for. This is advisory
	// metadata, like Model. The harness does not match the names against
	// its toolbox.
	Tools []string

	// Scope is the directory this definition governs. Empty means it
	// applies everywhere. A non-empty Scope qualifies the name:
	// "apps/web" and "reviewer" become "apps/web:reviewer". A definition
	// scoped to one directory then coexists with a same-named one
	// somewhere else instead of overriding it.
	Scope string

	// Source records where the definition came from — a file path, a
	// row ID — so errors can name it.
	Source string
}

// Skill is a named body of instructions the model can pull into a
// conversation on demand. The metadata is always in the system prompt.
// The body arrives only when the model runs the "skill" tool. That is
// what keeps a large skill library affordable.
type Skill struct {
	// Name identifies the skill and is what the model passes to the
	// "skill" tool. It must be unique within a resolver.
	Name string

	// Description tells the model when to run the skill. It is the
	// only part of the skill in the system prompt.
	Description string

	// Body is the literal skill content. Whatever resolved the skill
	// fills it in — a string in code, or the file body for a skill read
	// from disk. The body stays out of the system prompt and reaches the
	// conversation through the "skill" tool. Empty is valid: metadata
	// with nothing behind it.
	Body string

	// Dir is the directory that holds the files of the skill. Dir reaches
	// the model with the body, so the model can read the references of
	// the skill. Dir must be a path the agent can reach, usually an
	// absolute on-disk path. If there is no such path, Dir stays empty
	// and the skill tool omits it.
	Dir string

	// Scope is the directory this skill governs, which is not Dir. A
	// skill at apps/web/deploy has Dir "apps/web/deploy" and Scope
	// "apps/web". Empty means it applies everywhere. A non-empty Scope
	// qualifies the name the model sees, as in "apps/web:deploy". A skill
	// scoped to one directory then coexists with a same-named one
	// somewhere else instead of overriding it.
	Scope string

	// Source records where the definition came from.
	Source string
}

// Instructions is a document that governs how the agent works — an
// AGENTS.md, a house style guide.
//
// Documents have no name, so they neither qualify nor override. Every
// document a resolver returns is part of the resolution, in resolver
// order.
//
// Dir says which directory a document governs. It is discovery metadata,
// not a rule the harness enforces. [prompt.Default] writes the documents
// with an empty Dir into the prompt and leaves the rest to the caller.
// The reason is that a decision like "the agent is working in web/ now"
// means a match of tool arguments against directory names. That match has
// no correct answer the harness can give you. A builder or middleware
// that knows your product can give one.
type Instructions struct {
	// Dir is the directory the document governs, relative to the root of
	// the resolver. Empty means it governs the whole tree, which is the
	// only kind [prompt.Default] writes into the prompt.
	Dir string

	// Source records where the document came from — usually its path.
	Source string

	// Content is the document text.
	Content string
}

// AgentList is a literal [Agent] resolver, built by [Agents].
type AgentList []Agent

// Agents returns a resolver over a fixed list of agent definitions, for
// agents declared in code rather than discovered on disk.
func Agents(agents ...Agent) AgentList { return AgentList(agents) }

// Agents implements the agent resolver interface of the harness. The
// returned slice is a copy, so a caller cannot mutate the definitions
// behind the resolver.
func (l AgentList) Agents(context.Context) ([]Agent, error) {
	out := make([]Agent, len(l))
	copy(out, l)
	return out, nil
}

// SkillList is a literal [Skill] resolver, built by [Skills].
type SkillList []Skill

// Skills returns a resolver over a fixed list of skills.
func Skills(skills ...Skill) SkillList { return SkillList(skills) }

// Skills implements the skill resolver interface of the harness. It
// returns a copy of the list.
func (l SkillList) Skills(context.Context) ([]Skill, error) {
	out := make([]Skill, len(l))
	copy(out, l)
	return out, nil
}

// DocList is a literal [Instructions] resolver, built by [Docs].
type DocList []Instructions

// Docs returns a resolver over a fixed list of instruction documents.
// The name is Docs rather than Instructions, so the constructor does not
// collide with the [Instructions] type.
func Docs(docs ...Instructions) DocList { return DocList(docs) }

// Instructions implements the instruction resolver interface of the
// harness. It returns a copy of the list.
func (l DocList) Instructions(context.Context) ([]Instructions, error) {
	out := make([]Instructions, len(l))
	copy(out, l)
	return out, nil
}
