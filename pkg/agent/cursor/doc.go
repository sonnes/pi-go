// Package cursor implements [agent.Agent] by delegating each turn to the
// Cursor Agent CLI.
//
// Each Send runs `cursor-agent --print --output-format stream-json`. When the
// CLI reports a session ID, later Sends pass `--resume <session-id>` so Cursor
// can continue the same chat.
//
// Unlike the claude and codex agents, this package has no WithThinkingLevel
// option: the Cursor CLI exposes no reasoning-effort flag. Reasoning is bound
// to the model name instead (for example "sonnet-4.5-thinking"), so select a
// thinking-capable model through the model setting rather than a separate
// effort level.
package cursor
