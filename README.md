# Pi (Go)

[![Go Reference](https://pkg.go.dev/badge/github.com/sonnes/pi-go.svg)](https://pkg.go.dev/github.com/sonnes/pi-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/sonnes/pi-go)](https://goreportcard.com/report/github.com/sonnes/pi-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A provider-agnostic SDK for building AI agents in Go.

Pi supports text generation, structured output, image generation, tool calling, streaming, and usage tracking across multiple providers (Anthropic, OpenAI, Google). It is a Go port of [Mario Zechner](https://github.com/badlogic)'s [pi](https://github.com/badlogic/pi-mono) project.

## Design differences from pi-mono

Pi-go is a ground-up rewrite, not a transliteration. The core abstractions (messages, content blocks, events, tool calling) are aligned with pi-mono, but several architectural choices diverge to take advantage of Go's type system and concurrency model.

**Immutable construction.** Agent configuration is frozen at `New()`. There are no `setModel()` or `setTools()` setters. This eliminates data races when multiple goroutines observe the same agent and removes an entire class of "config changed mid-run" bugs. Pi-mono keeps agent state mutable with setters for model, tools, system prompt, and thinking level.

**Typed generics for tools.** `DefineTool[In, Out]` derives JSON schemas from Go types at init time using reflection. If a type can't be schematized, the constructor panics — you find out at startup, not mid-conversation. Pi-mono uses TypeBox schemas constructed at runtime with manual validation via AJV.

**Unified hook system.** A single `Hook` callback signature covers five lifecycle events (`BeforeCall`, `BeforeTool`, `AfterTool`, `AfterTurn`, `BeforeStop`). Multiple hooks per event chain in registration order with event-specific merging semantics. Pi-mono uses separate callback fields on the agent config, each with a different function signature.

**Composable system prompts.** The `prompt.Section` interface (`Key()` + `Content()`) supports dynamic, lazily-rendered prompt sections. Sections can read from databases, fetch context, or change between turns. Pi-mono uses a plain string with a setter.

**Per-tool parallel control.** Each tool declares whether it's safe for parallel execution via a `Parallel` flag. When all tools in a batch are marked parallel, they run concurrently; otherwise they run sequentially. Pi-mono uses a single global mode (`"sequential"` or `"parallel"`) for all tools.

**Pull-based event streaming with replay.** Agent events flow through a `pubsub.Broker` with a ring buffer. Multiple goroutines subscribe independently, and late subscribers replay buffered events via `After(seq)`. Pi-mono uses push-based `subscribe(fn)` callbacks.

**Streaming-first with dual consumption.** Every LLM call returns an `EventStream` that supports both patterns: iterate events with `Events()`, or block on the final message with `Result()`. `GenerateText` is literally `StreamText(...).Result()`. Pi-mono has separate `stream()` and `streamSimple()` methods with an async iterable plus a `.result()` promise.

**Interface-based agent.** The `Agent` interface defines the contract; `Default` is the standard implementation. The primary motivation was supporting CLI-based agents — wrapping `claude` as a subprocess agent that works with a Claude subscription rather than API keys. The interface also makes it straightforward to test with mock agents or build alternative implementations. Pi-mono has a single concrete class.

## Two-layer architecture

You can independently use the low-level `ai` package for direct LLM access, or the high-level `agent` package for an agentic loop.

### `ai` package — Direct LLM access

Provider-agnostic functions for text generation, structured output, image generation, and tool calling. Use this when you want full control over the conversation loop.

```go
import "github.com/sonnes/pi-go/pkg/ai"

msg, err := ai.GenerateText(ctx, model, ai.Prompt{
    System:   "You are a helpful assistant.",
    Messages: []ai.Message{ai.UserMessage("Hello!")},
})
```

### `agent` package — Agentic loop

Manages turn-based conversation, tool execution, event streaming, and lifecycle hooks. Use this for autonomous agents that call tools and make decisions over multiple turns.

```go
import "github.com/sonnes/pi-go/pkg/agent"

a := agent.New(
    agent.WithModel(model),
    agent.WithTools(weatherTool, searchTool),
    agent.WithSystemPrompt(myPrompt),
    agent.WithMaxTurns(10),
)
```

The agent is configured entirely via `agent.Option` values — `WithModel` (full `ai.Model`), `WithProvider` (bind an `ai.Provider` directly, bypassing the global registry), `WithModelName` (string only, for CLI-style agents that own their own model catalog), plus the standard `WithTools`, `WithHistory`, `WithSystemPrompt`, `WithStreamOpts`, `WithMaxTurns`, and `WithHook`.

### Agent factory registry

Agents can be constructed by string name through a small factory registry, mirroring the `ai.Provider` registry. Register once at startup, resolve anywhere:

```go
import (
    "github.com/sonnes/pi-go/pkg/agent"
    "github.com/sonnes/pi-go/pkg/agent/claude"
)

agent.RegisterFactory("claude", claude.Factory)

f, _ := agent.GetFactory("claude")
a := f(
    agent.WithModelName("sonnet"),
    claude.WithAllowedTools("Read", "Edit"),
)
```

Sub-package options (`claude.WithAllowedTools`, `claude.WithCLIPath`, ...) return `agent.Option` values, so agent-level and sub-package options compose in a single slice. Sub-package config lives under `agent.Config.Extensions` keyed by the sub-package name.

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/sonnes/pi-go/pkg/ai"
    "github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
    "github.com/sonnes/pi-go/pkg/agent"
    "github.com/sonnes/pi-go/pkg/pubsub"
)

// Define a typed tool.
type WeatherInput struct {
    City string `json:"city"`
}

type WeatherOutput struct {
    Temp string `json:"temp"`
}

var weatherTool = ai.DefineTool[WeatherInput, WeatherOutput](
    "get_weather",
    "Get current weather for a city",
    func(ctx context.Context, in WeatherInput) (WeatherOutput, error) {
        return WeatherOutput{Temp: "22°C"}, nil
    },
)

func main() {
    ctx := context.Background()

    // Register a provider.
    p := anthropic.New(anthropic.WithAPIKey("sk-..."))
    ai.RegisterProvider(p.API(), p)

    model := ai.Model{
        ID:  "claude-sonnet-4-20250514",
        API: "anthropic-messages",
    }

    // Create an agent with tools.
    a := agent.New(
        agent.WithModel(model),
        agent.WithTools(weatherTool),
        agent.WithMaxTurns(5),
    )
    defer a.Close()

    // Subscribe to events in a goroutine.
    go func() {
        ch := a.Subscribe(ctx)
        for pe := range ch {
            evt := pe.Payload()
            switch evt.Type {
            case agent.EventMessageUpdate:
                if evt.AssistantEvent != nil && evt.AssistantEvent.Type == ai.EventTextDelta {
                    fmt.Print(evt.AssistantEvent.Delta)
                }
            case agent.EventAgentEnd:
                fmt.Println()
            }
        }
    }()

    // Send a message and wait for completion.
    if err := a.Send(ctx, "What's the weather in Paris?"); err != nil {
        log.Fatal(err)
    }

    msgs, err := a.Wait(ctx)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Agent produced %d messages\n", len(msgs))
}
```

## Providers

| Provider                | Package                           | API Identifier       |
| ----------------------- | --------------------------------- | -------------------- |
| Anthropic Messages      | `pkg/ai/provider/anthropic`       | `anthropic-messages` |
| OpenAI Chat Completions | `pkg/ai/provider/openai`          | `openai-completions` |
| Google Gemini           | `pkg/ai/provider/google`          | `google-generative`  |
| Claude CLI              | `pkg/ai/provider/claudecli`       | `claude-cli`         |
| Gemini CLI              | `pkg/ai/provider/geminicli`       | `gemini-cli`         |
| OpenAI Responses        | `pkg/ai/provider/openairesponses` | `openai-responses`   |

Each provider is a separate Go module, so you only import (and depend on) the SDKs you use.

## Core concepts

**[Tools](docs/concepts/ai/tools.md)** — Define typed tools with `DefineTool[In, Out]`. JSON schemas are generated automatically from Go types. Tool errors become results the model can reason about, not Go errors.

**[Streaming](docs/concepts/agent/streaming.md)** — All operations stream by default. `EventStream` supports both iteration (`Events()`) and blocking (`Result()`). Agent events flow through a pub/sub broker with multi-subscriber support and replay.

**[Hooks](docs/concepts/agent/agent.md)** — Five lifecycle hooks (`BeforeCall`, `BeforeTool`, `AfterTool`, `AfterTurn`, `BeforeStop`) let you transform messages, deny tool execution, modify results, compact history, or inject follow-up messages without modifying the core loop.

**[Messages & content](docs/concepts/ai/messages.md)** — Three roles (`User`, `Assistant`, `ToolResult`) and four content types (`Text`, `Thinking`, `Image`, `ToolCall`). The agent layer adds extensible `CustomMessage` types via embedding.

**[Structured output](docs/concepts/ai/content.md)** — `GenerateObject[T]` produces typed objects with automatic JSON schema derivation. Providers that support structured output implement the `ObjectProvider` interface.

**[Models & usage](docs/concepts/ai/usage.md)** — `Model` carries provider routing, cost information, and capability flags. Usage tracking accumulates input/output/cache tokens and computes costs per request.

## Project structure

```
pkg/
├── ai/              # Core SDK: messages, streaming, tools, providers
│   └── provider/    # Provider implementations (anthropic, openai, google, ...)
├── agent/           # Agentic loop, hooks, event streaming
├── prompt/          # Composable system prompt sections
├── pubsub/          # Generic pub/sub broker with event replay
└── buffer/          # Generic ring buffer
```
