---
title: "Tools"
summary: "Defining tools, the Tool interface, execution flow, and result types"
read_when:
  - Defining or registering a tool
  - Working with tool execution or results
---

# Tools

Tools let models call Go functions. The SDK handles schema generation, JSON marshaling, and result formatting.

## Design decisions

**Typed generics over raw JSON.** `DefineTool[In, Out]` generates JSON Schema from Go types at creation time. Invalid types panic at startup — fail fast, not at runtime during a conversation.

**Tool errors are results, not Go errors.** When a tool function returns an error, the SDK converts it to a `ToolResult` with `IsError: true`. The model sees the error and can react (retry, explain, try a different approach). Go errors from `Run` indicate infrastructure failures (serialization, panics), not tool-level errors.

**Parallel-safe marking.** `DefineParallelTool` sets a flag in `ToolInfo` telling the model it can call this tool concurrently with others. The SDK doesn't enforce parallelism — it's a hint to the model.

## Tool interface

Any type implementing `Tool` (with `Info()` and `Run()` methods) can be used. `DefineTool` returns a `ToolDef` that implements this. Custom tools can implement the interface directly for full control over schema and execution.

## Execution flow

1. Model returns a `ToolCall` content block with `ID`, `Name`, and `Arguments`.
2. Agent matches the tool by name, creates a `ToolCallReq`.
3. `Run` deserializes input, calls the typed function, serializes output.
4. Errors from the function become `IsError: true` results visible to the model.

## Streaming progress

`ToolCallReq.OnUpdate` enables streaming partial results during long-running tool execution. The agent loop forwards these as `tool_execution_update` events.

## Output serialization

`string` → text result. `[]byte` → media result. Everything else → JSON-marshaled text.

## Related

- [Agent](/concepts/agent/agent) — how tools are wired into the agent loop
- [Content](/concepts/ai/content) — `ToolCall` content blocks in model responses
- [Streaming](/concepts/agent/streaming) — tool execution events
