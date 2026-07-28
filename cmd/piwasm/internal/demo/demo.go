// Package demo drives the browser demo: the real agent loop, the real
// typed tool, and the real session tree, over a provider that is either
// scripted (no credentials, deterministic) or live (the visitor's own
// OpenRouter key).
//
// Everything here is portable Go so it can be tested with `go test`.
// The syscall/js bridge that exposes it to the page is the only part
// built exclusively for wasm.
package demo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/session"
)

// Command kinds the page can send.
const (
	CmdRun     = "run"
	CmdBranch  = "branch"
	CmdCompact = "compact"
	CmdReset   = "reset"
	CmdKey     = "key"
)

// Event kinds the page receives.
const (
	KindStatus = "status"
	KindDelta  = "delta"
	KindTool   = "tool"
	KindTree   = "tree"
	KindError  = "error"
	KindDone   = "done"
)

// Modes the demo can run in.
const (
	ModeScripted = "scripted"
	ModeLive     = "live"
)

// Command is one instruction from the page.
type Command struct {
	Kind    string `json:"kind"`
	Text    string `json:"text,omitempty"`
	EntryID string `json:"entryId,omitempty"`
	Key     string `json:"key,omitempty"`
	Model   string `json:"model,omitempty"`
}

// Event is one update for the page. Unused fields are omitted so the
// JSON on the wire stays readable in devtools.
type Event struct {
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
	Tool   *Tool  `json:"tool,omitempty"`
	Tree   []Node `json:"tree,omitempty"`
	LeafID string `json:"leafId,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Model  string `json:"model,omitempty"`
}

// Tool describes one client-side tool call for the page.
type Tool struct {
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
}

// Node is one transcript entry in the shape the page renders.
type Node struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Text     string `json:"text"`
	Children []Node `json:"children,omitempty"`
}

// State is the demo's session state. The demo does not use it, but
// durable sessions are generic over one, and a session that carries
// nothing still has to carry something.
type State struct {
	Runs int `json:"runs"`
}

// WeatherInput is the tool's input — a Go type, not a hand-written
// schema. DefineTool derives the JSON schema from it at init.
type WeatherInput struct {
	City string `json:"city" jsonschema:"required"`
}

// WeatherOutput is what the tool returns.
type WeatherOutput struct {
	City string `json:"city"`
	Temp string `json:"temp"`
	Sky  string `json:"sky"`
}

// Demo owns one durable session and the provider behind it.
type Demo struct {
	emit  func(Event)
	store session.Store[State]

	mode  string
	model string
	lm    ai.LanguageModel

	// scripted is kept across resets so its cursor can be rewound.
	scripted *Scripted

	agent *durable.Agent[State]
	seq   int
}

// New builds a demo in scripted mode with a fresh in-memory session.
func New(ctx context.Context, emit func(Event)) (*Demo, error) {
	if emit == nil {
		emit = func(Event) {}
	}

	scripted := NewScripted(DefaultScript())
	scripted.Delay = StreamDelay

	d := &Demo{
		emit:     emit,
		store:    session.NewMemoryStore[State](),
		mode:     ModeScripted,
		model:    "scripted/demo",
		scripted: scripted,
		lm:       ai.NewLanguageModel(ai.Model{ID: "scripted/demo"}, scripted),
	}

	if err := d.open(ctx); err != nil {
		return nil, err
	}

	return d, nil
}

// StreamDelay paces scripted deltas so streaming is visible in a
// browser. Tests set it to zero through NewScripted directly.
var StreamDelay = 28 * time.Millisecond

// weatherTool is the demo's one tool. Its schema is derived from
// WeatherInput at init — an unschematizable type would panic here,
// at startup, rather than mid-conversation.
var weatherTool = ai.DefineTool(
	"get_weather",
	"Get the current weather for a city",
	func(_ context.Context, in WeatherInput) (WeatherOutput, error) {
		city := in.City
		if city == "" {
			city = "Paris"
		}

		return WeatherOutput{City: city, Temp: "22°C", Sky: "clear"}, nil
	},
)

// open starts a new durable session against the current provider.
func (d *Demo) open(ctx context.Context) error {
	d.seq++

	a, err := durable.New[State](
		ctx,
		d.lm,
		agent.WithTools(weatherTool),
		agent.WithMaxTurns(6),
		durable.WithStore(d.store),
		durable.WithSessionID(fmt.Sprintf("demo-%d", d.seq)),
	)
	if err != nil {
		return fmt.Errorf("demo: open session: %w", err)
	}

	d.agent = a

	return nil
}

// Close releases the underlying agent.
func (d *Demo) Close() error {
	if d.agent == nil {
		return nil
	}

	return d.agent.Close()
}

// Handle executes one command and emits the resulting events. Every
// command ends with a tree snapshot, so the page never has to track
// state itself.
func (d *Demo) Handle(ctx context.Context, c Command) error {
	var err error

	switch c.Kind {
	case CmdRun:
		err = d.run(ctx, c.Text)
	case CmdBranch:
		err = d.branch(ctx, c.EntryID)
	case CmdCompact:
		err = d.compact(ctx)
	case CmdReset:
		err = d.reset(ctx)
	case CmdKey:
		err = d.useKey(ctx, c.Key, c.Model)
	default:
		err = fmt.Errorf("demo: unknown command %q", c.Kind)
	}

	if err != nil {
		d.emit(Event{Kind: KindError, Text: err.Error()})

		return err
	}

	return d.snapshot(ctx)
}

// run sends one user message and streams the turn.
func (d *Demo) run(ctx context.Context, text string) error {
	if text == "" {
		text = "What's the weather in Paris?"
	}

	stream := d.agent.Run(ctx, ai.UserMessage(text))

	// A durable run lifts the inner agent's events, and forwards each
	// only once the entries it asserts are in the store — so anything
	// the page renders has already survived.
	for e, err := range stream.Events() {
		if err != nil {
			return fmt.Errorf("demo: run: %w", err)
		}
		if e.Type != durable.EventAgent || e.Agent == nil {
			continue
		}

		switch inner := *e.Agent; inner.Type {
		case agent.EventMessageUpdate:
			if inner.AssistantEvent != nil && inner.AssistantEvent.Delta != "" {
				d.emit(Event{Kind: KindDelta, Text: inner.AssistantEvent.Delta})
			}
		case agent.EventToolExecutionStart:
			d.emit(Event{Kind: KindTool, Tool: &Tool{Name: inner.ToolName, Args: inner.Args}})
		case agent.EventToolExecutionEnd:
			d.emit(Event{Kind: KindTool, Tool: &Tool{
				Name:   inner.ToolName,
				Result: fmt.Sprint(inner.Result),
			}})
		}
	}

	d.emit(Event{Kind: KindDone})

	return nil
}

// branch moves the leaf so the next run grows a sibling.
func (d *Demo) branch(ctx context.Context, entryID string) error {
	if entryID == "" {
		return errors.New("demo: branch needs an entry id")
	}

	if err := d.agent.Branch(ctx, entryID); err != nil {
		return fmt.Errorf("demo: branch: %w", err)
	}

	d.emit(Event{Kind: KindStatus, Text: "rewound to " + short(entryID)})

	return nil
}

// compact appends a summary of the path so far.
func (d *Demo) compact(ctx context.Context) error {
	if err := d.agent.Compact(ctx); err != nil {
		return fmt.Errorf("demo: compact: %w", err)
	}

	d.emit(Event{Kind: KindStatus, Text: "compacted — nothing deleted, rewind still works"})

	return nil
}

// reset abandons the session and starts a new one.
func (d *Demo) reset(ctx context.Context) error {
	if err := d.Close(); err != nil {
		return err
	}

	d.scripted.Reset()
	d.store = session.NewMemoryStore[State]()

	return d.open(ctx)
}

// snapshot emits the current tree and leaf.
func (d *Demo) snapshot(ctx context.Context) error {
	entries, err := d.agent.Entries(ctx)
	if err != nil {
		return fmt.Errorf("demo: entries: %w", err)
	}

	d.emit(Event{
		Kind:   KindTree,
		Tree:   nodesFrom(session.Tree(entries)),
		LeafID: d.agent.LeafID(),
		Mode:   d.mode,
		Model:  d.model,
	})

	return nil
}

// nodesFrom converts a session tree into the page's shape.
func nodesFrom(roots []*session.Node) []Node {
	out := make([]Node, 0, len(roots))
	for _, r := range roots {
		out = append(out, Node{
			ID:       r.Entry.Header().ID,
			Role:     roleOf(r.Entry),
			Text:     textOf(r.Entry),
			Children: nodesFrom(r.Children),
		})
	}

	return out
}

func roleOf(e session.Entry) string {
	switch entry := e.(type) {
	case session.MessageEntry:
		return string(entry.Message.Role)
	case session.CompactionEntry:
		return "compaction"
	default:
		return "entry"
	}
}

func textOf(e session.Entry) string {
	switch entry := e.(type) {
	case session.MessageEntry:
		return entry.Message.Text()
	case session.CompactionEntry:
		return entry.Summary
	default:
		return ""
	}
}

func short(id string) string {
	if len(id) <= 8 {
		return id
	}

	return id[:8]
}
