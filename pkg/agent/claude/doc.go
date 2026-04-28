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
//	    agent.WithModelName("opus"),
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
// All options — both [agent.Option] (e.g. [agent.WithModelName],
// [agent.WithMaxTurns], [agent.WithHistory], [agent.WithSystemPrompt])
// and claude-specific options (e.g. [WithCLIPath], [WithAllowedTools],
// [WithSessionID]) — satisfy the [agent.Option] type and pass through the
// same slice. [agent.WithSystemPrompt] is rendered to a string and passed
// to the subprocess as `--system-prompt`; use [WithAppendSystemPrompt] to
// append to the default system prompt instead.
//
// Factory registration:
//
//	// Register once at startup, then construct by name.
//	agent.RegisterFactory("claude", claude.Factory)
//
//	f, _ := agent.GetFactory("claude")
//	a := f(agent.WithModelName("sonnet"))
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
//
// # Limitations vs. the Default agent
//
// The Claude CLI subprocess owns the entire agent loop — tool dispatch,
// system prompts, and multi-turn state are managed internally by the
// CLI rather than by this package. As a result:
//
//   - Lifecycle hooks registered via [agent.WithHook] are NOT invoked.
//     [agent.HookBeforeCall], [agent.HookBeforeTool], [agent.HookAfterTool],
//     [agent.HookAfterTurn], and [agent.HookBeforeStop] all have no
//     effect when used with this agent. Use the [agent.Default] agent
//     if you need hooks.
//   - [agent.Agent.Continue] is not supported in stream-json mode; pair
//     [WithSessionID] with [Agent.Send] to resume a prior conversation.
//   - Tool execution events do not carry [agent.Event.PartialResult] —
//     the CLI does not surface in-flight tool progress over its stdout
//     protocol, so [agent.EventToolExecutionUpdate] is never emitted.
//   - [agent.EventMessageUpdate] is not emitted: each NDJSON assistant
//     line is a complete message, so the lifecycle goes directly from
//     message_start to message_end with no intermediate updates.
package claude
