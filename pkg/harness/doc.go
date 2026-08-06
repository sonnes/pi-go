// Package harness is the composition layer above [durable]. It takes
// the declarative artifacts an agent product is configured with —
// agent definitions, skills, instruction documents — and compiles them
// into loop mechanics: a system prompt, first-run entries, a
// synthesized skill tool, and per-run middleware.
//
// [New] returns a factory, not an agent:
//
//	h, err := harness.New(
//	    harness.WithCatalog(cat),
//	    harness.WithDefaultModel("anthropic/claude-sonnet-4-6"),
//	    harness.WithSkills(
//	        fs.Skills(builtin, "skills"), // compiled in, lowest
//	        fs.Skills(home, "skills"),    // the user's, wins a collision
//	    ),
//	    harness.WithTools(read.New(), write.New()),
//	)
//
//	a, err := h.Agent(ctx, durable.WithSessionID("s1")) // create or resume
//
// A factory rather than an agent because two things need one: one
// configuration serves many sessions, and seeding a fresh session means
// knowing at creation time whether it is fresh.
//
// # Resolvers
//
// Artifacts are discovered by resolvers — anything implementing
// [def.AgentResolver], [def.SkillResolver], or
// [def.InstructionResolver]. A resolver can be a filesystem convention,
// an embedded FS, a database query, or a literal list; [def.Agents],
// [def.Skills], and [def.Docs] cover definitions written in code.
//
// The interfaces live in [def], beside the types they return, so a
// resolver never imports the harness that consumes it.
//
// One resolver is one source. [WithAgents], [WithSkills], and
// [WithInstructions] take a list of them, lowest first, and repeated
// calls append:
//
//	harness.WithSkills(
//	    fs.Skills(builtin, "skills"),         // lowest
//	    fs.Skills(home, "skills"),
//	    fs.Skills(project, ".agents/skills"), // highest
//	)
//
// A source — project, user, global — is not a concept the harness has:
// it is a convention paired with a root, so a hierarchy is just the
// order of this list.
//
// # Names decide what survives
//
// Sources union, and the union is settled by name at build time.
//
// A skill with a [def.Skill.Scope] is qualified with the directory it
// governs — "apps/web" and "deploy" become "apps/web:deploy" — so
// same-named artifacts from different directories coexist rather than
// displacing one another. Qualification is structural: it follows from
// where the artifact lives, never from what else is registered, so
// adding a "deploy" at the root does not rename "apps/web:deploy".
//
// Two artifacts that still land on the same name are the same artifact
// declared twice, and the highest source wins — replacing the whole
// definition rather than merging it field by field. That is what lets a
// project replace a built-in skill by naming it. A name keeps the
// position it first appeared at, so an override never reshuffles the
// list the model is shown.
//
// Handing over one name twice within a single source is an error, not an
// override: overriding is between sources.
//
// Instructions have no name, so every document resolved applies, in
// order.
//
// Resolvers run on every build — each [Harness.Agent] and [Harness.Env]
// call — and the harness never caches across builds. A built agent keeps its
// snapshot for its whole lifetime, so its prompt is stable; the next
// build sees the current state of the disk. A resolver that wants
// caching implements it internally, where it knows what invalidates.
//
// # How artifacts become behavior
//
//   - Instruction documents are rendered into the system prompt in
//     resolver order — the ones with no [def.Instructions.Dir]. A
//     directory-bound document is resolved and handed to the builder,
//     which decides what it is worth.
//   - Skills appear in the prompt as names and descriptions; the
//     synthesized "skill" tool hands the model a body when it asks.
//   - Agent definitions resolve like skills and are handed over as
//     data — [prompt.Env.Agents] for builders, [Harness.Env] for
//     callers. No tool is synthesized from them, and their Model and
//     Tools fields are advisory: the harness neither resolves nor
//     validates them.
//   - Tools pass through to the loop untouched.
//
// "skill" and "agent" are reserved names, checked against the
// registered tools at [New]. ("agent" is reserved so the subagent tool
// can return without breaking anyone.)
//
// # Option currency
//
// Every harness option is an [agent.Option] writing an extension slot,
// the same currency [durable] uses. One flat list configures all three
// layers: the harness reads its slot and forwards the rest down through
// [durable.New] to [agent.New].
//
// The same options are accepted at two sites. [New] sets a baseline, and
// [Harness.Agent] overlays it for one build — because a working
// directory, a project's resolvers, and a session's extra tools are
// properties of the session, not of the process. That overlay is what
// lets a single harness serve many repositories instead of one harness
// per repository.
//
// Scalars override; everything else appends, baseline first. A resolver
// passed to [Harness.Agent] is an additional source on top of the
// baseline's, and being last it is the one that wins a name collision:
//
//	a, err := h.Agent(ctx,
//	    durable.WithSessionID(id),
//	    harness.WithWorkDir(repo),
//	    // this project, on top
//	    harness.WithSkills(fs.SkillsAt(repo, ".agents/skills")),
//	)
//
// # What comes back
//
// [Harness.Agent] returns a [durable.Agent] — the harness compiles the
// configuration and gets out of the way. There is no harness agent type
// wrapping it: seeding rides down as [durable.WithMiddleware], so every
// durable verb, [durable.Agent.Fork] included, behaves exactly as it
// does for an agent built by [durable.New].
//
// Per-run middleware belongs to [durable] for the same reason.
// [durable.WithMiddleware] travels down the flat option list without
// the harness touching it, and [durable.Fail] is how one refuses a run.
// Interception inside a run belongs to [agent.Hook], also forwarded
// untouched.
//
// See docs/concepts/harness.mdx for the design rationale.
package harness
