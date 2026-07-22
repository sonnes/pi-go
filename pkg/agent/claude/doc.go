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
//	    ai.Model{ID: "opus", Name: "opus"},
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
// The model is passed as New's first argument; all options — both
// [agent.Option] (e.g.
// [agent.WithMaxTurns], [agent.WithHistory], [agent.WithSystemPrompt])
// and claude-specific options (e.g. [WithCLIPath], [WithAllowedTools],
// [WithSessionID]) — satisfy the [agent.Option] type and pass through the
// same slice. [agent.WithSystemPrompt] is rendered to a string and, by
// default, passed to the subprocess as `--append-system-prompt`,
// layering onto Claude Code's own base prompt; use
// [WithAppendPrompt](false) to pass it as `--system-prompt` and replace
// the base prompt instead.
//
// Construction:
//
//	a := claude.New(ai.Model{ID: "sonnet", Name: "sonnet"})
//
// For spec-based creation, register [Factory] with the catalog under the
// "claude" kind:
//
//	cat.RegisterAgent("claude", claude.Factory())
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
//   - Of the lifecycle hooks registered via [agent.WithHook], only
//     [agent.HookBeforeTool] is invoked: it answers the CLI's
//     can_use_tool permission requests (the subprocess is launched
//     with `--permission-prompt-tool stdio` whenever such hooks are
//     registered; pair with [WithPermissionMode] to control which
//     calls the CLI asks about). A hook error or Deny blocks the tool
//     call. [agent.HookBeforeCall], [agent.HookAfterTool],
//     [agent.HookAfterTurn], and [agent.HookBeforeStop] have no
//     effect — the CLI owns those lifecycle points. Use the
//     [agent.Default] agent if you need them.
//   - [agent.Agent.Continue] is not supported in stream-json mode; pair
//     [WithSessionID] with [Agent.Send] to resume a prior conversation.
//   - Tool execution events do not carry [agent.Event.PartialResult] —
//     the CLI does not surface in-flight tool progress over its stdout
//     protocol, so [agent.EventToolExecutionUpdate] is never emitted.
//   - [agent.EventMessageUpdate] carries only the delta
//     ([agent.Event.AssistantEvent]); the accumulated
//     [agent.Event.Message] snapshot the Default agent attaches to each
//     update is not populated. The subprocess streams content-block
//     deltas (via --include-partial-messages) and the complete message
//     arrives on the assistant line that ends the block.
package claude
