// Package claude implements [agent.Agent] by delegating the agent loop
// to a long-lived Claude Code CLI subprocess.
//
// The subprocess is started lazily on first [Agent.Send] with
// `--input-format stream-json --output-format stream-json` and stays
// alive across turns: each Send writes one SDKUserMessage NDJSON line
// to stdin and blocks until the CLI emits the corresponding result
// line. The subprocess handles tool execution, system prompts, and
// multi-turn conversation state internally.
//
// Basic usage:
//
//	a := claude.New(
//	    claude.WithModel("opus"),
//	    claude.WithAllowedTools("Read", "Edit", "Bash"),
//	)
//	defer a.Close()
//
//	ch := a.Subscribe(ctx)
//	a.Send(ctx, "Fix the bug in main.go")
//	for pe := range ch {
//	    // handle pe.Payload() events...
//	}
//
// Rich content (images, multi-block messages) is supported by
// [Agent.SendMessages] — the last user message in the batch is
// serialized to an SDKUserMessage with an Anthropic content block array.
//
// Session resume:
//
//	// After a Send, the session ID is captured automatically.
//	sid := a.SessionID()
//
//	// Later, resume by seeding a new Agent with the session ID and
//	// calling Send. Continue is not supported in stream-json mode.
//	a2 := claude.New(claude.WithSessionID(sid))
//	a2.Send(ctx, "pick up where we left off")
package claude
