# Pi (Go)

[![Go Reference](https://pkg.go.dev/badge/github.com/sonnes/pi-go.svg)](https://pkg.go.dev/github.com/sonnes/pi-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/sonnes/pi-go)](https://goreportcard.com/report/github.com/sonnes/pi-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A provider-agnostic SDK for building AI agents in Go — from a single completion call to durable, session-backed agents that survive process restarts.

Pi supports text generation, structured output, image and speech generation, tool calling, streaming, usage tracking, and persistent sessions across multiple providers (Anthropic, OpenAI, Google, OpenRouter, and local CLI-backed agents).

## Origins

Pi-go started as a Go port of [Mario Zechner](https://github.com/badlogic)'s [pi](https://github.com/badlogic/pi-mono). The core vocabulary — messages, content blocks, streaming events, tool calling, the agentic loop — still traces back to pi-mono, and the two projects remain conceptually close. The API is now shaped by Go's type system and concurrency model rather than by the TypeScript original:

- **Typed generics instead of runtime schemas.** `DefineTool` and `GenerateObject[T]` derive JSON schemas from Go types at init time. If a type can't be schematized, the constructor panics — you find out at startup, not mid-conversation.
- **Immutable construction instead of setters.** Agent configuration is frozen at `New()`. No `setModel()`, no `setTools()`, no "config changed mid-run" bugs when multiple goroutines observe the same agent.
- **Run-scoped streams instead of a subscription broker.** A single verb drives the loop: `Run` appends messages and returns the run's `Stream`. Consume it event by event with `Events()` (a Go range-over-func iterator) or block on the result with `Wait()`. Errors — including pre-flight ones — surface on the stream, never as a panic or a lost run.
- **Interface-based agents.** `agent.Agent` is a contract, not a class. Subprocess CLIs (`claude`, `codex`, `cursor-agent`) plug in as first-class agents that work with subscription logins rather than API keys, and mocks are trivial.
- **Unified hooks and per-tool parallelism.** Five lifecycle events share one `Hook` signature, and each tool declares whether it is safe for parallel execution instead of a single global mode.

The newest layer, durable agents, is inspired by [Flue](https://github.com/withastro/flue), the Astro team's open agent framework and its durable-execution model: record every step of a session in a durable log, and any process can pick the conversation up exactly where it left off. Pi-go adapts that idea to Go — an append-only transcript tree behind a small `Store` interface, with branching, forking, and compaction all derived from a single leaf-pointer mechanism.

## Pick your layer

The SDK is a stack. Start at the top; drop down when you need control.

| Layer      | Package                      | Use it for                                                            |
| ---------- | ---------------------------- | --------------------------------------------------------------------- |
| Front door | `pkg/pi`                     | One import; providers auto-wired from environment credentials          |
| Registry   | `pkg/catalog`                | Own your catalog: providers, models, agent factories, spec resolution  |
| Agent loop | `pkg/agent`                  | Tools, hooks, turn management, run streams                             |
| Direct LLM | `pkg/ai`                     | Single calls: text, structured objects, images, speech                 |
| Durability | `pkg/durable`, `pkg/session` | Agents that survive restarts; branch, fork, compact                    |

`pkg/pi` owns a default catalog and auto-wires providers from environment credentials (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GOOGLE_API_KEY`, `OPENROUTER_API_KEY`, ...) on first use. Model specs are `"<provider>/<model>"`, or a bare model ID when exactly one registered provider serves it. For explicit control — multiple credentials, no globals, custom base URLs — construct providers directly and register them with your own `catalog.Catalog`.

## Quick start

Generate text with one import:

```go
import (
    "github.com/sonnes/pi-go/pkg/ai"
    "github.com/sonnes/pi-go/pkg/pi"
)

msg, err := pi.GenerateText(ctx, "claude-sonnet-4-5", ai.Prompt{
    Messages: []ai.Message{ai.UserMessage("Write a haiku about Go.")},
})
```

Run an agent with a typed tool and stream its events:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/sonnes/pi-go/pkg/agent"
    "github.com/sonnes/pi-go/pkg/ai"
    "github.com/sonnes/pi-go/pkg/pi"
)

type WeatherInput struct {
    City string `json:"city"`
}

type WeatherOutput struct {
    Temp string `json:"temp"`
}

var weatherTool = ai.DefineTool(
    "get_weather",
    "Get current weather for a city",
    func(ctx context.Context, in WeatherInput) (WeatherOutput, error) {
        return WeatherOutput{Temp: "22°C"}, nil
    },
)

func main() {
    ctx := context.Background()

    a, err := pi.Agent(
        "claude-sonnet-4-5",
        pi.WithTools(weatherTool),
        pi.WithMaxTurns(5),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer a.Close()

    s := a.Run(ctx, ai.UserMessage("What's the weather in Paris?"))
    for e, err := range s.Events() {
        if err != nil {
            log.Fatal(err)
        }
        if e.Type == agent.EventMessageUpdate && e.AssistantEvent != nil {
            fmt.Print(e.AssistantEvent.Delta)
        }
    }
    fmt.Println()
}
```

The same spec routes to different backends: `"claude/sonnet"` runs a Claude Code subprocess agent, `"codex/gpt-5-codex"` a Codex one, and a plain model ID runs the in-process loop against the provider's API.

## Durable agents

`pkg/durable` turns the agent loop into an agent that survives process restarts. The session ID is the memory boundary — the application decides what it means (a ticket number, a user ID, a thread key). The same ID always resumes the same conversation:

```go
// ChatState is whatever the app tracks per session; changes are
// logged in the transcript like everything else.
type ChatState struct {
    Title string
}

store := session.NewMemoryStore[ChatState]() // or fs.New (JSONL files), or your own Store

// Monday, process A.
da, err := pi.DurableAgent[ChatState](ctx, "claude-sonnet-4-5",
    durable.WithStore(store),
    durable.WithSessionID("user-42"),
)
_, _ = da.Run(ctx, ai.UserMessage("My name is Ravi. Remember it.")).Wait()
da.Close()

// Thursday, process B. Same ID — same conversation.
da, _ = pi.DurableAgent[ChatState](ctx, "claude-sonnet-4-5",
    durable.WithStore(store),
    durable.WithSessionID("user-42"),
)
msgs, _ := da.Run(ctx, ai.UserMessage("What's my name?")).Wait()
```

Following Flue's durable-execution model, persistence is per message: run input is persisted before the run starts, and every message the loop produces is persisted when it completes. Run events double as durability receipts — a `message_end` is forwarded only after its entries are in the store, so anything a consumer has seen complete survives a crash. If a crash leaves an assistant tool call without its results, the model view repairs it on resume by synthesizing interrupted tool results.

The transcript is an append-only tree with a leaf pointer marking the active position. History is never mutated or deleted, and one mechanism covers a family of operations:

- **`Branch`** moves the leaf to an earlier entry; the next turn grows a sibling. Edit, retry, and rewind are all branches.
- **`Fork`** lifts the active path into a separate session for what-if exploration.
- **`Compact`** appends a summary entry that shrinks the model context — nothing is deleted, rewind still works.
- **`Append`** persists application-defined entries (artifacts, UI state) in the tree without ever sending them to the model.

Sessions carry typed application state (`Session[T]` — a title, active model, whatever the app tracks), and the `Store` contract is small: implement it over your database, or use the built-in memory and filesystem (JSONL) stores.

## Providers

| Provider                | Package                           | API Identifier       |
| ----------------------- | --------------------------------- | -------------------- |
| Anthropic Messages      | `pkg/ai/provider/anthropic`       | `anthropic-messages` |
| OpenAI Chat Completions | `pkg/ai/provider/openai`          | `openai-completions` |
| OpenAI Responses        | `pkg/ai/provider/openairesponses` | `openai-responses`   |
| Google Gemini           | `pkg/ai/provider/google`          | `google-generative`  |
| Claude CLI              | `pkg/ai/provider/claudecli`       | `claude-cli`         |
| Codex CLI               | `pkg/ai/provider/codexcli`        | `codex-cli`          |
| Cursor CLI              | `pkg/ai/provider/cursorcli`       | `cursor-cli`         |

OpenRouter is served by the Responses adapter through a dialect flag. Each provider is a separate Go module, so you only import (and depend on) the SDKs you use. OAuth login flows — including reusing an existing Claude Code or Codex CLI subscription login — are documented in [docs/concepts/auth/oauth.md](docs/concepts/auth/oauth.md).

## Core concepts

**[Tools](docs/concepts/ai/tools.md)** — Define typed tools with `DefineTool`. JSON schemas are generated automatically from Go types. Tool errors become results the model can reason about, not Go errors.

**[Streaming](docs/concepts/agent/streaming.md)** — All operations stream by default. Every LLM call returns an `EventStream` and every agent run returns a `Stream`; both support iteration (`Events()`) and blocking (`Wait()`). `GenerateText` is literally `StreamText(...).Wait()`.

**[Hooks](docs/concepts/agent/agent.md)** — Five lifecycle hooks (`BeforeCall`, `BeforeTool`, `AfterTool`, `AfterTurn`, `BeforeStop`) let you transform messages, deny tool execution, modify results, compact history, or inject follow-up messages without modifying the core loop.

**[Messages & content](docs/concepts/ai/messages.md)** — Three roles (`User`, `Assistant`, `ToolResult`) and five content types (`Text`, `Thinking`, `Image`, `File`, `ToolCall`). `ToolCall` covers both client-executed function tools and provider-hosted [server tools](docs/capabilities/server-tools.md) (web search, code execution).

**[Structured output](docs/concepts/ai/content.md)** — `GenerateObject[T]` produces typed objects with automatic JSON schema derivation. Providers that support structured output implement the `ObjectProvider` interface.

**[Models & usage](docs/concepts/ai/usage.md)** — `Model` carries provider routing, cost information, and capability flags. Usage tracking accumulates input/output/cache tokens and computes costs per request.

## Project structure

```
pkg/
├── pi/          # Batteries-included front door: env auto-wiring, one-import helpers
├── catalog/     # Registry: providers, models, agent factories, spec resolution
├── agent/       # Agentic loop, hooks, run streams
│   ├── claude/  # Claude Code subprocess agent
│   ├── codex/   # Codex CLI subprocess agent
│   └── cursor/  # Cursor CLI subprocess agent
├── durable/     # Session-backed agents: resume, branch, fork, compact
├── session/     # Persistence primitives: Session, Entry tree, Store contract, fs store
├── stream/      # Generic push stream with iterator and blocking consumption
└── ai/          # Core SDK: messages, content, streaming, tools, capability interfaces
    └── provider/  # anthropic, openai, openairesponses, google, claudecli, codexcli, cursorcli
cmd/pi/          # CLI: provider login, interactive chat, agent test-drive
```

This is a Go workspace with a separate `go.mod` per provider and for `pkg/pi`. Run `make tidy` to update all modules.
