// Package agent provides an agentic loop on top of the [ai] SDK.
//
// It manages prompt building, tool execution, event streaming, and turn
// management. The [Agent] interface is the main entry point, with
// [Default] as the standard implementation.
//
// Configuration flows entirely through [Option] values applied at [New].
// Model routing uses [WithModel] (full [ai.Model], resolved via the global
// provider registry) or [WithProvider] (bind an [ai.Provider] instance
// directly). [WithModelName] sets a string identifier for agents that
// manage their own model catalog.
//
// Alternative [Agent] implementations register under string names via
// [RegisterFactory]. Callers resolve factories by name with [GetFactory]
// and construct agents through the same [Option] stream used by [New].
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
