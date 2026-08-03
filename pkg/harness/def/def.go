package def

import (
	"context"
)

// AgentResolver discovers agent definitions. It lives here rather than
// in the harness so that a resolver — a filesystem scan, a database
// query — depends on def alone and never on the harness that consumes
// it.
//
// One resolver is one source. The harness takes a list of them and
// unions what they return; see the package documentation.
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
// and advisory metadata about how it wants to run. The harness
// resolves, qualifies, and unions definitions exactly like skills, then
// hands them to the prompt builder and to the harness's Env callers as
// data. It synthesizes no tool from them: the v1 "agent" spawn tool was
// removed, and the name stays reserved for its return. What a product
// does with the resolved definitions — route to them, spawn them with
// its own machinery, list them in a UI — is its call.
type Agent struct {
	// Name identifies the agent. It must be unique within a resolver.
	Name string

	// Description tells a reader — human or model — what this agent is
	// for.
	Description string

	// Prompt is the agent's identity — the body of its system prompt,
	// for whatever runs it.
	Prompt string

	// Model is a catalog spec ("<provider>/<model>") the definition
	// asks for. Advisory metadata: the harness neither resolves nor
	// validates it — the product consuming the definition does.
	Model string

	// Tools names the tools the definition asks for. Advisory metadata,
	// like Model: the harness does not check the names against its
	// toolbox.
	Tools []string

	// Scope is the directory this definition governs. Empty means it
	// applies everywhere. A non-empty Scope qualifies the name —
	// "apps/web" and "reviewer" become "apps/web:reviewer" — so a
	// definition scoped to one directory coexists with a same-named one
	// somewhere else instead of overriding it.
	Scope string

	// Source records where the definition came from — a file path, a
	// row ID — so errors can name it.
	Source string
}

// Skill is a named body of instructions the model can pull into a
// conversation on demand. Its metadata is always in the system prompt;
// the body arrives only when the "skill" tool is invoked, which is what
// keeps a large skill library affordable.
type Skill struct {
	// Name identifies the skill and is what the model passes to the
	// "skill" tool. It must be unique within a resolver.
	Name string

	// Description tells the model when to invoke the skill. It is the
	// only part of the skill in the system prompt.
	Description string

	// Body is the literal skill content, populated by whatever resolved
	// the skill — a string in code, the file body for one read from
	// disk. It stays out of the system prompt and reaches the
	// conversation through the "skill" tool. Empty is valid: metadata
	// with nothing behind it.
	Body string

	// Dir is the directory the skill's files live in, handed to the
	// model alongside the body so it can read the skill's references.
	// It must be a path the agent can reach — an absolute on-disk path,
	// typically — and stays empty when there is no such path, in which
	// case the skill tool omits it.
	Dir string

	// Scope is the directory this skill governs, which is not Dir: a
	// skill at apps/web/deploy has Dir "apps/web/deploy" and Scope
	// "apps/web". Empty means it applies everywhere. A non-empty Scope
	// qualifies the name the model sees — "apps/web:deploy" — so a skill
	// scoped to one directory coexists with a same-named one somewhere
	// else instead of overriding it.
	Scope string

	// Source records where the definition came from.
	Source string
}

// Instructions is a document that governs how the agent works — an
// AGENTS.md, a house style guide.
//
// Documents have no name, so they neither qualify nor override: every
// document a resolver returns is part of the resolution, in resolver
// order.
//
// Dir says which directory a document governs, and it is discovery
// metadata rather than a rule the harness enforces. [prompt.Default]
// renders the documents with an empty Dir and leaves the rest to the
// caller, because deciding that an agent "is working in web/ now" means
// matching tool arguments against directory names, and that match has no
// correct answer the harness can make for you. A builder or middleware
// that knows your product can make it.
type Instructions struct {
	// Dir is the directory the document governs, relative to the
	// resolver's root. Empty means it governs the whole tree, which is
	// the only kind [prompt.Default] renders.
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

// Agents implements the harness's agent resolver interface. The
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

// Skills implements the harness's skill resolver interface, returning a
// copy of the list.
func (l SkillList) Skills(context.Context) ([]Skill, error) {
	out := make([]Skill, len(l))
	copy(out, l)
	return out, nil
}

// DocList is a literal [Instructions] resolver, built by [Docs].
type DocList []Instructions

// Docs returns a resolver over a fixed list of instruction documents.
// It is named Docs rather than Instructions so the constructor does not
// collide with the [Instructions] type.
func Docs(docs ...Instructions) DocList { return DocList(docs) }

// Instructions implements the harness's instruction resolver interface,
// returning a copy of the list.
func (l DocList) Instructions(context.Context) ([]Instructions, error) {
	out := make([]Instructions, len(l))
	copy(out, l)
	return out, nil
}
