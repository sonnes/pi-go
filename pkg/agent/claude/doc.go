// Package claude implements [agent.Agent] with a long-lived Claude Code
// CLI subprocess. The subprocess runs the agent loop.
//
// The package starts the subprocess on the first [Agent.Run] with
// `--input-format stream-json --output-format stream-json`. The
// subprocess stays alive across turns. Each Run writes one
// SDKUserMessage NDJSON line to stdin. Run then blocks until the CLI
// emits the matching result line. The subprocess runs the tools, holds
// the system prompts, and keeps the multi-turn conversation state.
//
// One Run is one line, whatever the number of messages it receives.
// The CLI starts a turn for every user line it reads, so a turn that
// carries injected context ahead of the message of the user must arrive
// as one line. Run concatenates the content blocks of the user messages
// in the order given. It skips a message that is not from the user: the
// CLI keeps its own transcript, and replaying an assistant message is
// not the business of this package.
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
//	a.Run(ctx, ai.UserMessage("Fix the bug in main.go"))
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
// [Agent.Run] accepts rich content, such as images and multi-block
// messages. It serializes the turn to an SDKUserMessage with an array of
// Anthropic content blocks.
//
// # Session identity
//
// The CLI names a session once and resumes it after that. The two are
// different flags, and passing the create flag for a session the CLI
// already holds is an error. [WithNewSession] creates, and
// [WithSessionID] resumes:
//
//	// First run of the session.
//	a := claude.New(model, claude.WithNewSession(id))
//
//	// Every run after it.
//	a := claude.New(model, claude.WithSessionID(id))
//
// The ID must be a UUID, which is the only shape the CLI accepts. The
// package reports a different shape before it starts a subprocess. A
// caller with session IDs of another shape needs a UUID of its own for
// this side.
//
// Naming the session lets one ID serve both sides. A caller that already
// has a session identity — a durable session, a ticket, a thread — does
// not have to store a second one. To read back the ID the CLI generated
// when the caller named none, use [Agent.SessionID] after the first run.
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
//   - History given with [agent.WithHistory] does not reach the model.
//     The CLI keeps the conversation itself and replays it from
//     [WithSessionID]. Only the messages of the Run go to the
//     subprocess.
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
