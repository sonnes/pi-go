// Package claude implements [agent.Agent] with a long-lived Claude Code
// CLI subprocess. The subprocess runs the agent loop.
//
// The package starts the subprocess on the first [Agent.Send] with
// `--input-format stream-json --output-format stream-json`. The
// subprocess stays alive across turns. Each Send writes one
// SDKUserMessage NDJSON line to stdin. Send then blocks until the CLI
// emits the matching result line. The subprocess runs the tools, holds
// the system prompts, and keeps the multi-turn conversation state.
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
// New takes the model as its first argument. All options satisfy the
// [agent.Option] type and go through the same slice. This includes the
// agent options, for example [agent.WithMaxTurns], [agent.WithHistory],
// and [agent.WithSystemPrompt]. It also includes the claude-specific
// options, for example [WithCLIPath], [WithAllowedTools], and
// [WithSessionID]. The package converts [agent.WithSystemPrompt] to a
// string. By default it gives that string to the subprocess as
// `--append-system-prompt`, which adds to Claude Code's own base prompt.
// To replace the base prompt instead, use [WithAppendPrompt](false). The
// package then gives the string as `--system-prompt`.
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
// [Agent.SendMessages] accepts rich content, such as images and
// multi-block messages. It serializes the last user message in the batch
// to an SDKUserMessage with an array of Anthropic content blocks.
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
// # Limits compared to the Default agent
//
// The Claude CLI subprocess owns the full agent loop. The CLI, not this
// package, controls the tool dispatch, the system prompts, and the
// multi-turn state. As a result:
//
//   - Of the lifecycle hooks registered with [agent.WithHook], the
//     package runs only [agent.HookBeforeTool]. That hook answers the
//     can_use_tool permission requests from the CLI. If such hooks are
//     registered, the package starts the subprocess with
//     `--permission-prompt-tool stdio`. Use [WithPermissionMode] to
//     control which calls the CLI asks about. A hook error or a Deny
//     blocks the tool call. [agent.HookBeforeCall],
//     [agent.HookAfterTool], [agent.HookAfterTurn], and
//     [agent.HookBeforeStop] have no effect, because the CLI owns those
//     lifecycle points. If you need them, use the [agent.Default] agent.
//   - stream-json mode does not support [agent.Agent.Continue]. To
//     resume an earlier conversation, pair [WithSessionID] with
//     [Agent.Send].
//   - Tool execution events do not carry [agent.Event.PartialResult].
//     The stdout protocol of the CLI gives no progress for a tool that
//     still runs. As a result, the package never emits
//     [agent.EventToolExecutionUpdate].
//   - [agent.EventMessageUpdate] carries only the delta in
//     [agent.Event.AssistantEvent]. It does not carry the accumulated
//     [agent.Event.Message] snapshot that the Default agent attaches to
//     each update. The subprocess streams content-block deltas with
//     --include-partial-messages. The complete message arrives on the
//     assistant line that ends the block.
package claude
