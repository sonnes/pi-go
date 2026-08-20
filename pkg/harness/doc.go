// Package harness is the composition layer above [durable]. It takes
// the declarative artifacts of an agent product — agent definitions,
// skills, and instruction documents. It compiles them into loop
// mechanics: a system prompt, first-run entries, a synthesized skill
// tool, and per-run middleware.
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
// The harness is a factory and not an agent, because two things need a
// factory. One configuration serves many sessions. To seed a fresh
// session, the harness must know at creation time that the session is
// fresh.
//
// # Resolvers
//
// Resolvers discover the artifacts. A resolver is any type that
// implements [def.AgentResolver], [def.SkillResolver], or
// [def.InstructionResolver]. A resolver can be a filesystem convention,
// an embedded FS, a database query, or a literal list. [def.Agents],
// [def.Skills], and [def.Docs] cover definitions written in code.
//
// The interfaces live in [def], beside the types they return, so a
// resolver never imports the harness that consumes it.
//
// One resolver is one source. [WithAgents], [WithSkills], and
// [WithInstructions] take a list of resolvers, lowest first. Repeated
// calls append:
//
//	harness.WithSkills(
//	    fs.Skills(builtin, "skills"),         // lowest
//	    fs.Skills(home, "skills"),
//	    fs.Skills(project, ".agents/skills"), // highest
//	)
//
// A source — project, user, global — is not a concept in the harness. A
// source is a convention paired with a root. A hierarchy is therefore
// the order of this list.
//
// # Names decide what survives
//
// Sources combine, and the name settles the result at build time.
//
// The harness qualifies the name of a skill that has a
// [def.Skill.Scope]. The qualified name carries the directory the skill
// governs: "apps/web" and "deploy" become "apps/web:deploy". Artifacts
// with the same name in different directories therefore coexist, and
// neither one displaces the other. Qualification is structural. It
// follows from where the artifact lives, never from what else is
// registered. A new "deploy" at the root does not rename
// "apps/web:deploy".
//
// Two artifacts that still land on the same name are the same artifact
// declared twice. The highest source wins. It replaces the whole
// definition and does not merge it field by field. A project can
// therefore replace a built-in skill when it declares the same name. A
// name keeps the position where it first appeared, so an override never
// reshuffles the list the model sees.
//
// One source that hands over the same name twice is an error, not an
// override. An override is always between two sources.
//
// Instructions have no name. Every document a resolver returns applies,
// in order.
//
// Resolvers run on every build — on each [Harness.Agent] and
// [Harness.Env] call — and the harness caches nothing across builds. A
// built agent keeps its snapshot for its whole lifetime, so its prompt
// is stable. The next build sees the current state of the disk. A
// resolver that wants caching implements caching internally, where it
// knows what invalidates the cache.
//
// # How artifacts become behavior
//
//   - The prompt builder puts instruction documents into the system
//     prompt in resolver order — the documents with no
//     [def.Instructions.Dir]. The harness resolves a directory-bound
//     document and hands it to the builder, which decides what the
//     document is worth.
//   - Skills appear in the prompt as names and descriptions. When the
//     model asks for a body, the synthesized "skill" tool hands one
//     over.
//   - Agent definitions resolve like skills, and the harness hands them
//     over as data — [prompt.Env.Agents] for builders, [Harness.Env]
//     for callers. The harness synthesizes no tool from them. Their
//     Model and Tools fields are advisory: the harness does not resolve
//     them and does not make sure that they are correct.
//   - Tools pass through to the loop untouched.
//
// "skill" and "agent" are reserved names. [New] makes sure that no
// registered tool claims either one. ("agent" stays reserved so the
// subagent tool can return without breaking anyone.)
//
// # Option currency
//
// Every harness option is an [agent.Option] that writes an extension
// slot. [durable] uses the same currency. One flat list configures all
// three layers: the harness reads its own slot and forwards the rest
// down through [durable.New] to the loop.
//
// The kind of the default model decides which loop that is. The harness
// gives [durable.New] a factory that calls [catalog.Catalog.Agent], and
// that method routes the spec. A registered agent kind, for example a
// Claude Code subprocess, wins its prefix. Every other spec becomes the
// default in-process loop over its language model.
//
// The harness compiles the same prompt and the same tool list for both.
// What the loop does with them is the business of the loop. A
// subprocess CLI owns its own tool dispatch, so it can accept the
// prompt and ignore the tools.
//
// Two sites accept the same options. [New] sets a baseline, and
// [Harness.Agent] overlays that baseline for one build. A working
// directory, the resolvers of a project, and the extra tools of a
// session are properties of the session, not of the process. This
// overlay lets one harness serve many repositories, instead of one
// harness for each repository.
//
// Scalars override. Everything else appends, baseline first. A resolver
// passed to [Harness.Agent] is an additional source above the baseline
// sources. It comes last, so it wins a name collision:
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
// [Harness.Agent] returns a [durable.Agent]. The harness compiles the
// configuration and then gets out of the way. No harness agent type
// wraps the result. Seeding rides down as [durable.WithMiddleware], so
// every durable verb behaves exactly as it does for an agent from
// [durable.New]. This includes [durable.Agent.Fork].
//
// Per-run middleware belongs to [durable] for the same reason. The
// option [durable.WithMiddleware] travels down the flat option list,
// and the harness does not touch it. [durable.Fail] refuses a run.
// Interception inside a run belongs to [agent.Hook], which the harness
// also forwards untouched.
//
// See docs/concepts/harness.mdx for the design rationale.
package harness
