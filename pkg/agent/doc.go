// Package agent provides an agentic loop on top of the [ai] SDK.
//
// It manages prompt building, tool execution, event streaming, and turn
// management. The [Agent] interface is the main entry point, with
// [Default] as the standard implementation. A single verb drives the
// loop: [Agent.Run] appends messages and returns the run's [Stream],
// which is consumed event by event ([Stream.Events]) or awaited as a
// whole ([Stream.Wait]). [Prompt] wraps Run+Wait for the common
// send-text-get-answer case.
//
// [New] takes the model as its first argument; the rest of the
// configuration flows through [Option] values. [Default] resolves the
// provider from the global [ai] registry by [ai.Model.Provider], or
// [WithProvider] binds an [ai.Provider] instance directly and skips the
// lookup.
//
// Alternative [Agent] implementations register under string names via
// [RegisterAgent]. Callers look one up by name with [GetAgent], or
// build an agent from a "<provider>/<model>" spec with [Create].
// Sub-packages (e.g. pkg/agent/claude) attach their own configuration
// via [WithExtension] and [WithExtensionMutator]; see [Config.Extensions]
// for the key convention.
//
// The loop is extensible via lifecycle hooks registered with [WithHook].
// Five [HookEvent] values cover the full lifecycle: [HookBeforeCall],
// [HookBeforeTool], [HookAfterTool], [HookAfterTurn], and [HookBeforeStop].
// All hooks share a single [Hook] callback signature with event-specific
// fields on [HookInput] and [HookOutput].
package agent
