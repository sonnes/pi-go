// Package codex implements [agent.Agent]. Each turn goes to the Codex CLI
// in non-interactive JSONL mode.
//
// The first Send starts a new thread with `codex exec --json`. When the CLI
// reports a thread ID, each later Send uses
// `codex exec resume --json <thread-id>`. The Codex CLI then owns the
// long-running context.
//
// The package normalizes the Codex JSONL items that map cleanly to pi-go
// concepts. Command executions become bash tool execution events. Native
// todo_list updates become TodoWrite tool-call and result messages.
package codex
