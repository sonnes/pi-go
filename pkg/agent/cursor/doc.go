// Package cursor implements [agent.Agent]. Each turn goes to the Cursor
// Agent CLI.
//
// Each Send runs `cursor-agent --print --output-format stream-json`. When
// the CLI reports a session ID, each later Send passes
// `--resume <session-id>`. Cursor then continues the same chat.
//
// This package has no WithThinkingLevel option, unlike the claude and codex
// agents. The Cursor CLI has no reasoning-effort flag. It binds reasoning
// to the model name instead (for example "sonnet-4.5-thinking"). Select a
// thinking-capable model with the model setting, not with a separate
// effort level.
package cursor
