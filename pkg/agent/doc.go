// Package agent provides an agentic loop on top of the [ai] SDK.
//
// It manages prompt building, tool execution, event streaming, and turn
// management. The [Agent] interface is the main entry point, with
// [Default] as the standard implementation.
//
// The loop is extensible via lifecycle hooks registered with [WithHook].
// Five [HookEvent] values cover the full lifecycle: [HookBeforeCall],
// [HookBeforeTool], [HookAfterTool], [HookAfterTurn], and [HookBeforeStop].
// All hooks share a single [Hook] callback signature with event-specific
// fields on [HookInput] and [HookOutput].
package agent
