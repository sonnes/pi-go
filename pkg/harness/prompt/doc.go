// Package prompt owns the contract between a harness and the text it
// puts in front of a model. The contract is the [Env] snapshot a harness
// build produces, the [Builder] and [Seeder] function types that consume
// it, and the default implementations of both.
//
// The contract lives here rather than in the harness. A custom builder
// then does not have to import the machinery that calls it. prompt
// depends on def, ai, and session. It must never import harness.
package prompt
