// Package def holds the declarative artifacts a harness compiles into
// loop mechanics — agent definitions, skills, instruction documents. It
// also holds the interfaces that discover them.
//
// The types are plain data with no behavior, and the package imports
// nothing outside the standard library. That is deliberate. A resolver —
// anything that discovers artifacts, from a filesystem scan to a database
// query — depends on def alone, never on the harness that consumes it.
// [AgentResolver] and its siblings live here for the same reason.
//
// Agent definitions are resolved data, not behavior. The harness
// qualifies and combines them exactly like skills. It then hands the result
// to the prompt builder and to Env callers. The harness synthesizes no
// tool from them. The v1 "agent" spawn tool was removed, and the tool
// name stays reserved so the feature can return. Until then, a product
// decides what to do with a resolved [Agent].
//
// # Sources and scope
//
// A resolver is one source. Several sources are a list, registered
// lowest first. The harness combines them at build time:
//
//	harness.WithAgents(
//	    fs.Agents(builtin, "agents"),         // lowest
//	    fs.Agents(home, "agents"),
//	    fs.Agents(project, ".agents/agents"), // highest
//	)
//
// Names decide what survives that union. The harness qualifies an
// artifact that has an [Agent.Scope] or [Skill.Scope] with the directory
// it governs. "apps/web" and "deploy" become "apps/web:deploy", so
// same-named artifacts from different directories coexist. Two artifacts
// that still land on the same name are the same artifact declared twice.
// The highest source wins. It replaces the whole definition and does not
// merge it field by field.
//
// Qualification is structural. It follows from where an artifact lives,
// never from what else is registered. A new "deploy" at the root does not
// rename "apps/web:deploy".
package def
