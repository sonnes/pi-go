// Package claude implements [agent.Agent] by delegating the agent loop
// to a Claude Code CLI subprocess. Each Send or Continue call spawns
// a new `claude --print` process. The subprocess handles tool execution,
// system prompts, and multi-turn conversation management internally.
//
// Basic usage:
//
//	a := claude.New(
//	    claude.WithModel("opus"),
//	    claude.WithAllowedTools("Read", "Edit", "Bash"),
//	)
//	stream := a.Send(ctx, "Fix the bug in main.go")
//	for evt, err := range stream.Events(ctx) {
//	    // handle events...
//	}
//
// Session resume:
//
//	// After a Send, the session ID is captured automatically.
//	sid := a.SessionID()
//
//	// Later, resume with:
//	a2 := claude.New(claude.WithSessionID(sid))
//	stream := a2.Continue(ctx)
package claude
