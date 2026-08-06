---
title: "Overview"
summary: "What pi-go is, how the packages stack, and where to look for what"
read_when:
  - Arriving at the documentation for the first time
  - Deciding which layer of the SDK to build against
  - Looking for the boundary between these docs and the API reference
---

Pi is a provider-agnostic SDK for building AI agents in Go. It covers the
whole range between one completion call and a durable, session-backed
agent that resumes after the process it started in is gone.

## The stack

The SDK is a stack, not a framework. Start at the top and drop down when
you need control; every layer is an ordinary Go package that works on its
own, and the layer below it is always reachable.

| Layer      | Package                      | Use it for                                                            |
| ---------- | ----------------------------- | --------------------------------------------------------------------- |
| Front door | `pkg/pi`                     | One import; providers auto-wired from environment credentials          |
| Registry   | `pkg/catalog`                | Own your catalog: providers, models, agent factories, spec resolution  |
| Agent loop | `pkg/agent`                  | Tools, hooks, turn management, run streams                             |
| Direct LLM | `pkg/ai`                     | Single calls: text, structured objects, images, speech                 |
| Durability | `pkg/durable`, `pkg/session` | Persistent transcripts with branching, forking, and compaction        |

Choosing a layer is mostly a question of how much wiring you want to own.
`pkg/pi` decides things for you — which providers exist, where credentials
come from, what a bare model ID means. `pkg/catalog` hands those decisions
back. Below that, `pkg/agent` and `pkg/ai` have no opinion about
configuration at all.

For durable agents, start with [Sessions](concepts/durable/sessions.md) for
the ownership boundary. Then read [Entries](concepts/durable/entries.md),
[Transcript Tree](concepts/durable/tree.md), and
[Durable Events](concepts/durable/events.md) for input visibility,
branching, and notifications.

## What these docs are for

**Concepts** explain why each piece is shaped the way it is: what a run
stream belongs to, why tool schemas are derived at init, what the session
ID actually means. Read them when you want the model behind the API.

**Capabilities** compare pi-go against the upstream provider APIs,
feature by feature. Read them when you need to know whether something is
supported, on which provider, and how far the support goes.

Neither is an API reference. Every type and function is documented in
GoDoc and published at
[pkg.go.dev/github.com/sonnes/pi-go](https://pkg.go.dev/github.com/sonnes/pi-go);
these pages link to it rather than restating it, so the signatures you
read are always the ones you compile against.

## Conventions on these pages

Each page carries a `summary` and a `read_when` list in its frontmatter.
The summary becomes the page's lede; `read_when` becomes the callout under
the title. Both exist so you can decide from the first screen whether the
page is the one you want — skimming a documentation site is a search
problem, and the answer should not be buried in paragraph four.
