// Package cursor implements [agent.Agent] by delegating each turn to the
// Cursor Agent CLI.
//
// Each Send runs `cursor-agent --print --output-format stream-json`. When the
// CLI reports a session ID, later Sends pass `--resume <session-id>` so Cursor
// can continue the same chat.
package cursor
