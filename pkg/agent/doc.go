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
// [New] takes an [ai.LanguageModel] as its first argument — a model
// already bound to a provider — and the rest of the configuration flows
// through [Option] values.
//
// Alternative [Agent] implementations (e.g. the subprocess CLIs) are
// exposed as an [agent.Factory] and registered with the catalog, which
// routes "<kind>/<model>" specs to them. Sub-packages (e.g. pkg/agent/claude)
// attach their own configuration via [WithExtension] and
// [WithExtensionMutator]; see [Config.Extensions] for the key convention.
//
// The loop is extensible via lifecycle hooks registered with [WithHook].
// Five [HookEvent] values cover the full lifecycle: [HookBeforeCall],
// [HookBeforeTool], [HookAfterTool], [HookAfterTurn], and [HookBeforeStop].
// All hooks share a single [Hook] callback signature with event-specific
// fields on [HookInput] and [HookOutput].
package agent
