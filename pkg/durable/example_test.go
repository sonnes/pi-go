// These examples document the intended developer experience of the
// durable package. They have no Output comments, so `go test` compiles
// them without running them — the API shape is validated at compile
// time while the implementation is still landing.
package durable_test

import (
	"context"
	"fmt"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/session"
)

// ChatState is the example session state — whatever the app tracks per
// session. Changes to it are logged as a single session.StateEntry.
type ChatState struct {
	Title string
	Model string
}

// OrderQuery and OrderStatus are the typed input/output of the
// example tool.
type OrderQuery struct {
	OrderID string `json:"order_id"`
}

type OrderStatus struct {
	Status string `json:"status"`
}

// newAssistant returns the example [ai.LanguageModel]: a fixed model
// bound to a mock provider that replies with canned text, so the
// examples run without network access.
func newAssistant() ai.LanguageModel {
	return ai.NewLanguageModel(
		ai.Model{ID: "claude-sonnet-4-6"},
		&mockProvider{responses: []*ai.EventStream{
			textStream("Your order shipped."),
			textStream("Your name is Ravi."),
			textStream("Done."),
			textStream("Sure."),
		}},
	)
}

// Example shows the full flow: a session addressed by ID, resumed
// across processes.
func Example() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	lookupOrder := ai.DefineTool(
		"lookup_order_status",
		"Look up the current fulfillment status for one order ID.",
		func(_ context.Context, q OrderQuery) (OrderStatus, error) {
			return OrderStatus{Status: "shipped"}, nil
		},
	)

	// The session ID is the memory boundary: the application decides
	// what it means — a ticket number, user ID, or thread key. The
	// same ID always resumes the same conversation. Agent options like
	// tools and system prompt ride alongside the durable options.
	da, err := durable.New[ChatState](
		ctx,
		newAssistant(),
		durable.WithStore(store),
		durable.WithSessionID("ticket-8472"),
		agent.WithSystemPrompt("Help customers check order status."),
		agent.WithTools(lookupOrder),
	)
	if err != nil {
		panic(err)
	}
	defer da.Close()

	msgs, err := da.Run(ctx, ai.UserMessage("Where is order order_1234?")).Wait()
	if err != nil {
		panic(err)
	}
	fmt.Println(msgs[len(msgs)-1].Text())
}

// ExampleNew shows durability: the same session ID picks up the
// conversation where it left off, even across process restarts.
func ExampleNew() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	// Monday, process A.
	da, _ := durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("user-42"))
	_, _ = da.Run(ctx, ai.UserMessage("My name is Ravi. Remember it.")).Wait()
	da.Close()

	// Thursday, process B. Same ID — same conversation.
	da, _ = durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("user-42"))
	defer da.Close()

	msgs, _ := da.Run(ctx, ai.UserMessage("What's my name?")).Wait()
	fmt.Println(msgs[len(msgs)-1].Text())
}

// ArtifactEntry is an application-defined entry persisted in the
// transcript tree without ever reaching the model.
type ArtifactEntry struct {
	session.CustomEntry
	Title   string
	Content string
}

// ExampleAgent_Append shows persisting custom application entries in
// the session transcript.
func ExampleAgent_Append() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	da, _ := durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("doc-7"))
	defer da.Close()

	err := da.Append(ctx, ArtifactEntry{
		CustomEntry: session.CustomEntry{Kind: "artifact"},
		Title:       "draft",
		Content:     "# Proposal\n...",
	})
	if err != nil {
		panic(err)
	}

	// Later — from any process.
	entries, _ := da.Entries(ctx)
	artifacts := session.Filter[ArtifactEntry](entries)
	fmt.Println(len(artifacts))
}

// ExampleAgent_Branch shows in-place branching: move the leaf, and
// the next turn grows a sibling. The abandoned branch stays in the
// tree.
func ExampleAgent_Branch() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	da, _ := durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("ticket-8472"))
	defer da.Close()

	// A checkpoint is just a remembered leaf.
	checkpoint := da.LeafID()
	_, _ = da.Run(ctx, ai.UserMessage("Reship the order.")).Wait()

	// Rewind and take the other approach.
	if err := da.Branch(ctx, checkpoint); err != nil {
		panic(err)
	}
	_, _ = da.Run(ctx, ai.UserMessage("Refund instead of reshipping.")).Wait()

	// Both branches are still in the tree.
	entries, _ := da.Entries(ctx)
	for _, root := range session.Tree(entries) {
		fmt.Println(len(root.Children))
	}
}

// ExampleAgent_Fork shows lifting the active path into a separate
// session for what-if exploration.
func ExampleAgent_Fork() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	da, _ := durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("ticket-8472"))
	defer da.Close()

	alt, err := da.Fork(ctx, "ticket-8472-refund")
	if err != nil {
		panic(err) // ErrSessionExists if the ID is taken
	}
	defer alt.Close()

	_, _ = alt.Run(ctx, ai.UserMessage("What if we refund instead of reshipping?")).Wait()
	fmt.Println(alt.Session().ParentID)
}

// ExampleAgent_Compact shows shrinking the model context without
// deleting history: a summary entry is appended and the context
// builder does the rest.
func ExampleAgent_Compact() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	da, _ := durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("user-42"))
	defer da.Close()

	// Months-old session? Summarize everything but the recent turns.
	// Nothing is deleted — rewind still works.
	if err := da.Compact(ctx, durable.KeepTurns(4)); err != nil {
		panic(err)
	}
	fmt.Println(len(da.Messages()))
}

// ExampleAgent_Run shows streaming: the run stream is turn-scoped —
// lifted inner agent events with persistence receipts riding the
// boundary events. Session events go to the publisher (see
// [ExampleWithPublisher]).
func ExampleAgent_Run() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	da, _ := durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("chat-1"))
	defer da.Close()

	s := da.Run(ctx, ai.UserMessage("Tell me a joke."))
	for e, err := range s.Events() {
		if err != nil {
			panic(err)
		}
		switch {
		case e.Agent.Type == agent.EventMessageUpdate:
			fmt.Print(e.Agent.AssistantEvent.Delta)
		case e.Agent.Type == agent.EventMessageEnd:
			// Receipt: the message's entries are in the store.
			fmt.Printf("persisted %d entries at leaf %s\n", len(e.Entries), e.LeafID)
		}
	}
}

// ExampleWithPublisher shows session-event delivery: the application
// injects a publisher and owns what happens next — forward to a
// websocket, log, or fan out. Events arrive at the mutation, not on
// the next run.
func ExampleWithPublisher() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	pub := durable.PublisherFunc(func(e durable.Event) {
		fmt.Printf("%s leaf=%q\n", e.Type, e.LeafID)
	})

	da, _ := durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("chat-2"), durable.WithPublisher(pub))
	defer da.Close()

	// → session_init leaf=""
	_ = da.SetState(ctx, ChatState{Title: "Jokes"})
	// → session_updated leaf="<state entry id>"
}

// ExampleAgent_SetState shows logging a session-state change — here a
// title and model edit — as a single session.StateEntry. State is
// last-wins and survives across processes; branching does not revert it.
func ExampleAgent_SetState() {
	store := session.NewMemoryStore[ChatState]()
	ctx := context.Background()

	da, _ := durable.New[ChatState](ctx, newAssistant(), durable.WithStore(store), durable.WithSessionID("ticket-8472"))
	defer da.Close()

	err := da.SetState(ctx, ChatState{
		Title: "Refund for order 1234",
		Model: "claude-opus-4-6",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(da.Session().State.Title)
}
