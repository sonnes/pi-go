// Package prompt owns the contract between a harness and the text it
// puts in front of a model: the [Env] snapshot a harness build produces,
// the [Builder] and [Seeder] function types that consume it, and the
// default implementations of both.
//
// The contract lives here rather than in the harness so that writing a
// custom builder does not mean importing the machinery that calls it.
// prompt depends on def, ai, and session; it must never import harness.
package prompt
