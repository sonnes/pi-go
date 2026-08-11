// Package agent provides an agentic loop on top of the [ai] SDK.
//
// The package builds prompts, runs tools, streams events, and manages
// turns. The [Agent] interface is the main entry point. [Default] is the
// standard implementation.
//
// A single verb drives the loop. [Agent.Run] appends messages and returns
// the [Stream] of the run. The caller reads that stream event by event
// with [Stream.Events], or waits for the whole result with [Stream.Wait].
// [Prompt] wraps Run and Wait for the common send-text-get-answer case.
//
// [New] takes an [ai.LanguageModel] as its first argument — a model that
// is already bound to a provider. The rest of the configuration flows
// through [Option] values.
//
// Other [Agent] implementations, for example the subprocess CLIs, come as
// an [agent.Factory] that registers with the catalog. The catalog routes
// "<kind>/<model>" specs to them.
//
// Sub-packages such as pkg/agent/claude attach their own configuration
// with [WithExtension] and [WithExtensionMutator]. For the key
// convention, see [Config.Extensions]. [Slot] is the typed way to work
// with one such slot. It creates options and reads the value back without
// manual type assertions.
//
// Lifecycle hooks extend the loop. Register a hook with [WithHook]. Five
// [HookEvent] values cover the full lifecycle: [HookBeforeCall],
// [HookBeforeTool], [HookAfterTool], [HookAfterTurn], and
// [HookBeforeStop]. All hooks share one [Hook] callback signature. Each
// event has its own fields on [HookInput] and [HookOutput].
package agent
