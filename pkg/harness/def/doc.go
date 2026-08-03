// Package def holds the declarative artifacts a harness compiles into
// loop mechanics — agent definitions, skills, instruction documents —
// together with the interfaces that discover them.
//
// The types are plain data with no behavior, and the package imports
// nothing outside the standard library. That is deliberate: a resolver
// — anything that discovers artifacts,
// from a filesystem scan to a database query — depends on def alone,
// never on the harness that consumes it. [AgentResolver] and its
// siblings live here for the same reason.
//
// Agent definitions are resolved data, not behavior. The harness
// qualifies and unions them exactly like skills and hands the result to
// the prompt builder and to Env callers — it synthesizes no tool from
// them. The v1 "agent" spawn tool was removed; the tool name stays
// reserved so the feature can return. Until then, what a product does
// with a resolved [Agent] is the product's decision.
//
// # Sources and scope
//
// A resolver is one source. Several sources are a list, registered
// lowest first, and the harness unions them at build time:
//
//	harness.WithAgents(
//	    fs.Agents(builtin, "agents"),         // lowest
//	    fs.Agents(home, "agents"),
//	    fs.Agents(project, ".agents/agents"), // highest
//	)
//
// Names decide what survives that union. An artifact with an
// [Agent.Scope] or [Skill.Scope] is qualified with the directory it
// governs — "apps/web" and "deploy" become "apps/web:deploy" — so
// same-named artifacts from different directories coexist. Two that
// still land on the same name are the same artifact declared twice, and
// the highest source wins, replacing the whole definition rather than
// merging it field by field.
//
// Qualification is structural: it follows from where an artifact lives,
// never from what else happens to be registered. Adding a "deploy" at
// the root does not rename "apps/web:deploy".
package def
