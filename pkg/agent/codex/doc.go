// Package codex implements [agent.Agent] by delegating each turn to the
// Codex CLI in non-interactive JSONL mode.
//
// The first Send starts a new thread with `codex exec --json`. When the CLI
// reports a thread ID, later Sends use `codex exec resume --json <thread-id>`
// so the Codex CLI owns the long-running context.
package codex
