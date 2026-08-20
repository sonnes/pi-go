package durable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/sonnes/pi-go/pkg/stream"
)

// slot is the [agent.Config] extension slot that durability reads its
// options from. The With* options of this package are [agent.Option]
// values that layer onto a single ext value. A durable agent therefore
// uses the same option currency as [agent.New].
//
// The slot holds an ext by value. An option list that never mentioned
// durability therefore reads back as the zero ext, not as nothing at
// all.
var slot = agent.Slot[ext]{Key: "durable"}

// ext accumulates durability configuration from the durable options.
type ext struct {
	store      session.Store
	sessionID  string
	publisher  Publisher
	middleware []Middleware
}

// mutate returns an option that layers f onto the ext of the slot.
func mutate(f func(*ext)) agent.Option {
	return slot.Mutate(func(e ext) ext {
		f(&e)
		return e
	})
}

// Agent is a persistent agent instance. It is an inner [agent.Agent]
// loop bound to a durable session ID. [New] creates every Agent, and
// direct construction is not supported. The agent owns transcript
// state only. Application metadata lives in the storage of the
// application, keyed by the session ID.
//
// Persistence is per message. The package persists run input before the
// run starts. It persists every message the loop produces when the
// message_end of that message arrives. The package forwards the lifted
// event only after the append succeeds, and the event carries the
// entries as its receipt. A crash leaves the store consistent at the
// last completed message. Resume repairs a dangling tool call (see
// [Agent.Messages]).
type Agent struct {
	store session.Store
	// factory builds the inner loop for each run. It is nil for an agent
	// that only records: [Agent.Append] and the read verbs work without a
	// loop, and [Agent.Run] reports the absence.
	factory   Factory
	baseOpts  []agent.Option
	publisher Publisher

	// run is the middleware chain of this instance around runBase. The
	// package builds it once at construction. It is never nil: an agent
	// with no registered middleware gets runBase itself.
	run Runner

	mu        sync.Mutex
	sessionID string
	entries   []session.Entry
	index     map[string]session.Entry
	leafID    string
	running   bool
	closed    bool
}

// newInner builds a fresh inner agent loop from the bound [Factory]. It
// uses the base options plus any extra options (for example, loaded
// history).
//
// An agent with no factory reports it here rather than at construction.
// A caller that only repairs or reads a transcript needs a session, not
// a loop, so the absence is an error for the verbs that run and for no
// other verb.
func (a *Agent) newInner(extra ...agent.Option) (agent.Agent, error) {
	if a.factory == nil {
		return nil, errors.New("durable: agent has no factory and cannot run")
	}
	opts := make([]agent.Option, 0, len(a.baseOpts)+len(extra))
	opts = append(opts, a.baseOpts...)
	opts = append(opts, extra...)
	return a.factory(opts...)
}

// WithStore sets the backing store. Without this option, [New] uses a
// fresh in-memory store, which is enough for tests and ephemeral
// agents. A persistent store is necessary to survive restarts.
func WithStore(s session.Store) agent.Option {
	return mutate(func(e *ext) { e.store = s })
}

// WithSessionID sets the session ID to create or resume. Without this
// option, [New] generates one. [Agent.SessionID] returns it.
func WithSessionID(id string) agent.Option {
	return mutate(func(e *ext) { e.sessionID = id })
}

// WithPublisher sets the [Publisher] that receives session lifecycle
// events. A forked agent inherits it.
func WithPublisher(p Publisher) agent.Option {
	return mutate(func(e *ext) { e.publisher = p })
}

// publish delivers a lifecycle event when a publisher is configured.
// Callers must not hold a.mu, because Publish can call back into the
// agent.
func (a *Agent) publish(evt Event) {
	if a.publisher != nil {
		a.publisher.Publish(evt)
	}
}

// New returns a durable [Agent]. If the session does not exist, New
// creates it. If the session exists, New loads its history from the
// store. The resume point is the last appended entry. New publishes
// [EventSessionInit] after the session is ready.
//
// f builds the inner loop of each run. [Model] wraps an
// [ai.LanguageModel] for the ordinary case. A nil f is valid and gives
// an agent that records but never runs: [Agent.Append], [Agent.Branch],
// and the read verbs work, and [Agent.Run] reports the missing loop. A
// caller that repairs an interrupted transcript needs exactly that.
//
// Everything else is optional. Without [WithStore], the session lives
// in a fresh in-memory store. Without [WithSessionID], New generates a
// session ID.
//
// New does not track instances. Each call returns a fresh instance, and
// the caller owns instance discipline. Two live instances of the same
// session cannot corrupt it. Each instance appends from its own leaf,
// so concurrent instances grow sibling branches in the tree.
func New(
	ctx context.Context,
	f Factory,
	opts ...agent.Option,
) (*Agent, error) {
	dcfg := slot.From(agent.ApplyOptions(opts...))

	store := dcfg.store
	if store == nil {
		store = session.NewMemoryStore()
	}

	sessionID := dcfg.sessionID
	if sessionID == "" {
		sessionID = newEntryID()
	}

	entries, err := store.LoadEntries(ctx, sessionID)
	switch {
	case errors.Is(err, session.ErrSessionNotFound):
		if cerr := store.CreateSession(ctx, sessionID, ""); cerr != nil {
			return nil, cerr
		}
		entries = nil
	case err != nil:
		return nil, err
	}

	leaf := ""
	if len(entries) > 0 {
		leaf = entries[len(entries)-1].Header().ID
	}

	index := make(map[string]session.Entry, len(entries))
	for _, e := range entries {
		index[e.Header().ID] = e
	}

	a := &Agent{
		store:     store,
		factory:   f,
		baseOpts:  opts,
		publisher: dcfg.publisher,
		sessionID: sessionID,
		entries:   entries,
		index:     index,
		leafID:    leaf,
	}
	a.run = chain(dcfg.middleware, a.runBase)

	a.publish(Event{
		Type:      EventSessionInit,
		SessionID: sessionID,
		LeafID:    leaf,
	})
	return a, nil
}

// Run persists entries at the leaf, then starts the inner loop. Run
// persists each message the loop produces before it forwards the
// message_end of that message. With zero entries, Run continues from
// the current leaf.
//
// Input is entries, not messages, so the caller controls what the model
// and the transcript each see. [Text], [Image], and [File] build the
// ordinary user entry. The model reads injected context, and the
// transcript hides it. Injected context comes in two forms that differ
// only in durability: Meta persists, and [Ephemeral] never leaves
// memory. A custom entry is the opposite of both. It persists, but Run
// never sends it to the model. Run assigns the tree fields on append.
//
// Entries reach the model in the order given. Run writes every kind
// except ephemeral as given. Compaction belongs in [Agent.Compact].
//
// The persistence receipts on the boundary events carry only durable
// entries. An ephemeral entry is therefore absent from them.
// [Agent.Entries] returns it instead.
//
// The stream is turn-scoped. It carries inner agent events lifted under
// [EventAgent], with persistence receipts on the boundary events. If a
// persist operation returns an error, the run fails loudly.
//
// Any [Middleware] registered with [WithMiddleware] wraps this call.
// The chain runs synchronously, on the goroutine of the caller, before
// the producer starts. A middleware that adds entries therefore adds
// them before the stream exists. A middleware that refuses the run
// returns [Fail], and no producer starts.
func (a *Agent) Run(ctx context.Context, entries ...session.Entry) *Stream {
	return a.run(ctx, entries...)
}

// runBase is the durable run itself. It is the innermost [Runner].
// [New] and [Agent.Fork] wrap the middleware chain around it.
func (a *Agent) runBase(ctx context.Context, entries ...session.Entry) *Stream {
	return stream.New(func(push func(Event)) ([]ai.Message, error) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			return nil, errors.New("durable: agent is closed")
		}
		if a.running {
			a.mu.Unlock()
			return nil, errors.New("durable: run already active")
		}
		a.running = true
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
		}()

		// History is the conversation before this turn, and nothing else.
		// The input of the turn is not part of it. A dangling tool call
		// is therefore repaired here, where it belongs: it is the
		// footprint of a run that crashed, never of the input the caller
		// just wrote.
		a.mu.Lock()
		history := repairToolCalls(session.ModelView(session.PathFrom(a.index, a.leafID)))
		a.mu.Unlock()

		// The entries of this turn reach the loop as the messages of the
		// run, in the order the caller wrote them, which keeps an
		// ephemeral entry in position. Splitting them from history is
		// what lets a loop treat the two differently. An in-process loop
		// appends the messages to its history and cannot tell them
		// apart; a subprocess CLI resumes its own transcript and needs
		// the turn on its own.
		input := inputMessages(entries)

		// Build the loop before anything reaches the store. A factory
		// that cannot start — a missing CLI, an unresolvable model — then
		// leaves the transcript exactly as it was, and the next run
		// starts from the same leaf. A run that cannot happen must not
		// record an input that never got an answer.
		inner, err := a.newInner(agent.WithHistory(history...))
		if err != nil {
			return nil, err
		}
		defer inner.Close()

		// Persist input before the run starts. Ephemeral entries do not
		// go to the store. They reach the model as run messages only.
		var inputEntries []session.Entry
		if len(entries) > 0 {
			inputEntries, err = a.persist(ctx, entries...)
			if err != nil {
				return nil, fmt.Errorf("durable: persist input: %w", err)
			}
		}

		s := inner.Run(ctx, input...)
		for evt, err := range s.Events() {
			if err != nil {
				return nil, err
			}
			out := Event{Type: EventAgent, Agent: &evt}

			switch evt.Type {
			case agent.EventAgentStart:
				out.Entries = inputEntries
				out.LeafID = a.LeafID()

			case agent.EventMessageEnd:
				if evt.Message != nil {
					persisted, perr := a.persist(ctx, session.NewMessageEntry(*evt.Message))
					if perr != nil {
						return nil, fmt.Errorf("durable: persist message: %w", perr)
					}
					out.Entries = persisted
					out.LeafID = a.LeafID()
				}
			}

			push(out)
		}
		return s.Wait()
	})
}

// Messages returns the model view of the active path: root to leaf,
// compaction-aware, meta entries included, custom entries excluded. A
// dangling tool call comes from a crash between an assistant message
// and its tool results. Messages repairs each one with a synthesized
// interrupted result.
func (a *Agent) Messages() []ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.modelViewLocked()
}

// Close marks the instance closed. A closed agent rejects further runs.
// A call to [New] with the same session ID resumes the session.
func (a *Agent) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// --- durable verbs ---

// SessionID returns the durable session ID this agent is bound to.
func (a *Agent) SessionID() string { return a.sessionID }

// LeafID returns the ID of the entry that the leaf pointer is on. For a
// fresh session, LeafID returns an empty string. To rewind, capture the
// ID before a risky turn and pass it to [Agent.Branch]. A checkpoint is
// a remembered leaf.
func (a *Agent) LeafID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.leafID
}

// Entries returns the full log of this instance in append order. The
// log holds meta entries, custom entries, and the [Ephemeral] entries
// that never reached the store. The log can therefore be wider than
// what a resume loads. [session.Tree] derives the tree, and
// [Agent.Transcript] returns the display view of the active path.
func (a *Agent) Entries(ctx context.Context) ([]session.Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]session.Entry, len(a.entries))
	copy(out, a.entries)
	return out, nil
}

// Transcript returns the active path for display: root to leaf, meta
// entries hidden, custom entries included.
func (a *Agent) Transcript(ctx context.Context) ([]session.Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	path := session.PathFrom(a.index, a.leafID)
	return session.TranscriptView(path), nil
}

// Append persists custom entries at the leaf without a run of the loop.
// Append never sends custom entries to the model. It records entries
// marked [Ephemeral] in memory, but it does not write them to the
// store.
func (a *Agent) Append(ctx context.Context, entries ...session.Entry) error {
	_, err := a.persist(ctx, entries...)
	return err
}

// Branch moves the leaf pointer to an earlier entry. The move is
// zero-copy and stays in the session. The next turn grows a sibling
// branch, and the abandoned branch stays in the tree. Edit, retry, and
// rewind are all this one operation.
//
// The leaf position lives in memory. A reopened session resumes at the
// last appended entry.
//
// Branch publishes [EventSessionBranched] after it moves the leaf.
func (a *Agent) Branch(ctx context.Context, entryID string) error {
	a.mu.Lock()

	if _, ok := a.index[entryID]; !ok {
		a.mu.Unlock()
		return fmt.Errorf("durable: entry %q not found", entryID)
	}

	fromID := a.leafID
	a.leafID = entryID
	a.mu.Unlock()

	a.publish(Event{
		Type:   EventSessionBranched,
		FromID: fromID,
		LeafID: entryID,
	})
	return nil
}

// Fork copies the active path into a new session with the given ID. It
// returns a fresh [Agent] bound to that session. The new session
// records this one as [session.Session.ParentID]. If the ID is taken,
// Fork returns [session.ErrSessionExists].
//
// The child inherits the publisher. A successful fork publishes
// [EventSessionForked] for the source, then [EventSessionInit] for the
// child.
func (a *Agent) Fork(ctx context.Context, newID string) (*Agent, error) {
	a.mu.Lock()
	path := session.PathFrom(a.index, a.leafID)
	a.mu.Unlock()

	if err := a.store.CreateSession(ctx, newID, a.sessionID); err != nil {
		return nil, err
	}

	// Chain the path again with fresh IDs. Timestamps carry over.
	copied := make([]session.Entry, len(path))
	parent := ""
	for i, e := range path {
		h := session.EntryHeader{
			ID:        newEntryID(),
			ParentID:  parent,
			CreatedAt: e.Header().CreatedAt,
		}
		copied[i] = withHeader(e, h)
		parent = h.ID
	}
	if len(copied) > 0 {
		if err := a.store.AppendEntries(ctx, newID, copied...); err != nil {
			return nil, err
		}
	}

	index := make(map[string]session.Entry, len(copied))
	for _, e := range copied {
		index[e.Header().ID] = e
	}

	child := &Agent{
		store:     a.store,
		factory:   a.factory,
		baseOpts:  a.baseOpts,
		publisher: a.publisher,
		sessionID: newID,
		entries:   copied,
		index:     index,
		leafID:    parent,
	}
	// The child builds its own chain and does not share the chain of
	// this agent. Each Middleware func runs again, so the closures that
	// hold per-run state belong to the child. A shared closure would let
	// the state of one session decide the runs of another session.
	child.run = chain(middlewareFrom(a.baseOpts), child.runBase)

	a.publish(Event{
		Type:      EventSessionForked,
		SessionID: newID,
		ParentID:  a.sessionID,
	})
	child.publish(Event{
		Type:      EventSessionInit,
		SessionID: newID,
		LeafID:    parent,
	})
	return child, nil
}

// defaultCompactPrompt instructs the summarizer when the caller
// supplies no [CompactPrompt] override.
const defaultCompactPrompt = "Summarize the conversation so far. Preserve every fact, decision, and open question in a compact form."

// CompactOption configures one [Agent.Compact] call.
type CompactOption func(*compactConfig)

type compactConfig struct {
	keepTurns int
	prompt    string
}

// KeepTurns keeps the most recent n turns out of the summary. A turn
// starts at a non-meta user message.
func KeepTurns(n int) CompactOption {
	return func(c *compactConfig) { c.keepTurns = n }
}

// CompactPrompt overrides the instruction that the summarizer agent
// receives. The default is [defaultCompactPrompt].
func CompactPrompt(s string) CompactOption {
	return func(c *compactConfig) { c.prompt = s }
}

// Compact appends a [session.CompactionEntry] that summarizes older
// turns on the active path. Compact removes nothing, so the full tree
// stays rewindable. An ephemeral agent from the factory of the session
// writes the summary. Custom entries pass through untouched. Compact
// publishes [EventSessionCompacted] after it appends the summary.
func (a *Agent) Compact(ctx context.Context, opts ...CompactOption) error {
	cfg := compactConfig{
		prompt: defaultCompactPrompt,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	a.mu.Lock()
	path := session.PathFrom(a.index, a.leafID)
	var userIdx []int
	for i, e := range path {
		me, ok := session.AsMessageEntry(e)
		if ok && !me.Meta && me.Message.Role == ai.RoleUser {
			userIdx = append(userIdx, i)
		}
	}

	if len(userIdx) == 0 || len(userIdx) <= cfg.keepTurns {
		a.mu.Unlock()
		return nil
	}

	firstKeptID := ""
	compactPath := path
	if cfg.keepTurns > 0 {
		firstKeptIdx := userIdx[len(userIdx)-cfg.keepTurns]
		firstKeptID = path[firstKeptIdx].Header().ID
		compactPath = path[:firstKeptIdx]
	}
	toSummarize := session.ModelView(compactPath)
	a.mu.Unlock()

	summarizer, err := a.newInner(agent.WithHistory(repairToolCalls(toSummarize)...))
	if err != nil {
		return fmt.Errorf("durable: compact: %w", err)
	}
	defer summarizer.Close()
	reply, err := agent.Prompt(
		ctx,
		summarizer,
		cfg.prompt,
	)
	if err != nil {
		return fmt.Errorf("durable: compact summarizer: %w", err)
	}

	a.mu.Lock()
	entries, err := a.persistLocked(ctx, session.CompactionEntry{
		Summary:     reply.Text(),
		FirstKeptID: firstKeptID,
	})
	if err != nil {
		a.mu.Unlock()
		return err
	}
	leafID := a.leafID
	a.mu.Unlock()

	a.publish(Event{
		Type:    EventSessionCompacted,
		Entries: entries,
		LeafID:  leafID,
	})
	return nil
}

// --- internals ---

// inputMessages projects run input for the model. It returns the
// message entries in the order given, ephemeral entries included.
// Custom entries carry no message, so inputMessages skips them. This
// matches [session.ModelView].
func inputMessages(entries []session.Entry) []ai.Message {
	var msgs []ai.Message
	for _, e := range entries {
		if me, ok := session.AsMessageEntry(e); ok {
			msgs = append(msgs, me.Message)
		}
	}
	return msgs
}

// persist assigns tree headers to entries, chains them at the leaf,
// appends them to the store, and advances the leaf. It returns the
// entries that reached the store. The returned slice is therefore a
// durability receipt.
//
// persist records entries marked [session.MessageEntry.Ephemeral] in
// the in-memory log, but it never hands them to the store. This is the
// one place that decides it, whichever verb the entries arrived
// through. An ephemeral entry hangs off the current leaf without an
// advance of the leaf, which keeps the entry off the durable chain.
//
// A stored entry must never name a parent that the store does not have.
// On resume, the walk in [session.PathFrom] would stop at such an entry
// and lose everything above it. An entry off the chain is also off the
// active path, so the transcript, the model view, and [Agent.Fork] skip
// it for free.
func (a *Agent) persist(ctx context.Context, entries ...session.Entry) ([]session.Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.persistLocked(ctx, entries...)
}

func (a *Agent) persistLocked(ctx context.Context, entries ...session.Entry) ([]session.Entry, error) {
	now := time.Now()
	parent := a.leafID
	recorded := make([]session.Entry, 0, len(entries))
	durables := make([]session.Entry, 0, len(entries))
	for _, e := range entries {
		h := session.EntryHeader{
			ID:        newEntryID(),
			ParentID:  parent,
			CreatedAt: now,
		}
		withID := withHeader(e, h)
		recorded = append(recorded, withID)

		me, isMessage := session.AsMessageEntry(withID)
		if isMessage && me.Ephemeral {
			continue
		}
		durables = append(durables, withID)
		parent = h.ID
	}

	if len(recorded) == 0 {
		return nil, nil
	}

	if len(durables) > 0 {
		if err := a.store.AppendEntries(ctx, a.sessionID, durables...); err != nil {
			return nil, err
		}
	}

	a.entries = append(a.entries, recorded...)
	for _, e := range durables {
		a.index[e.Header().ID] = e
	}
	a.leafID = parent
	return durables, nil
}

// modelViewLocked projects the active path for the model and repairs
// dangling tool calls. Callers must hold a.mu.
func (a *Agent) modelViewLocked() []ai.Message {
	path := session.PathFrom(a.index, a.leafID)
	return repairToolCalls(session.ModelView(path))
}

// repairToolCalls synthesizes interrupted tool results for assistant
// tool calls that have none. Such a call is the footprint of a crash
// between the persistence of an assistant message and the persistence
// of its tool results. Providers reject a tool call with no result, so
// resume repairs it.
func repairToolCalls(msgs []ai.Message) []ai.Message {
	var out []ai.Message
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		out = append(out, m)
		if m.Role != ai.RoleAssistant {
			continue
		}

		missing := make(map[string]string)
		var order []string
		for _, tc := range m.ToolCalls() {
			if tc.Server {
				continue
			}
			missing[tc.ID] = tc.Name
			order = append(order, tc.ID)
		}
		if len(order) == 0 {
			continue
		}

		for i+1 < len(msgs) && msgs[i+1].Role == ai.RoleToolResult {
			i++
			out = append(out, msgs[i])
			delete(missing, msgs[i].ToolCallID)
		}
		for _, id := range order {
			if name, ok := missing[id]; ok {
				out = append(out, ai.ErrorToolResultMessage(id, name, "tool execution interrupted"))
			}
		}
	}
	return out
}

// withHeader returns a copy of e with a new embedded
// [session.EntryHeader]. A custom entry reaches the header through its
// embedded [session.CustomEntry].
func withHeader(e session.Entry, h session.EntryHeader) session.Entry {
	v := reflect.New(reflect.TypeOf(e)).Elem()
	v.Set(reflect.ValueOf(e))
	if f := findHeaderField(v); f.IsValid() && f.CanSet() {
		f.Set(reflect.ValueOf(h))
	}
	return v.Interface().(session.Entry)
}

var headerType = reflect.TypeFor[session.EntryHeader]()

func findHeaderField(v reflect.Value) reflect.Value {
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	for i := range t.NumField() {
		if t.Field(i).Type == headerType {
			return v.Field(i)
		}
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			if inner := findHeaderField(v.Field(i)); inner.IsValid() {
				return inner
			}
		}
	}
	return reflect.Value{}
}

// newEntryID returns a random 16-hex-char entry ID.
func newEntryID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("durable: entropy unavailable: %v", err))
	}
	return hex.EncodeToString(b[:])
}
