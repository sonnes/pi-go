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

// extKey is the [agent.Config] extension slot durability reads its
// options from. Durable's With* options are [agent.Option]s that layer
// onto a single ext value, so a durable agent is configured with the
// same option currency as [agent.New].
const extKey = "durable"

// ext accumulates durability configuration from the durable options.
type ext struct {
	store     any // the session.Store[T], asserted in New
	sessionID string
	publisher Publisher
}

func extFrom(v any) *ext {
	if e, ok := v.(*ext); ok {
		return e
	}
	return &ext{}
}

// Agent is a persistent agent instance: an inner [agent.Agent] loop
// bound to a [session.Session]. Created by [New], never directly. The
// type parameter T is the session's application-defined state (see
// [session.Session]).
//
// Persistence is per message: run input is persisted before the run
// starts, and every message the loop produces is persisted when its
// message_end arrives — the lifted event is forwarded only after the
// append succeeds, carrying the entries as its receipt. A crash leaves
// the store consistent at the last completed message; a dangling tool
// call is repaired on resume (see [Agent.Messages]).
type Agent[T any] struct {
	store     session.Store[T]
	lm        ai.LanguageModel
	baseOpts  []agent.Option
	publisher Publisher

	mu      sync.Mutex
	sess    *session.Session[T]
	entries []session.Entry
	index   map[string]session.Entry
	leafID  string
	running bool
	closed  bool
}

// newInner builds a fresh inner agent loop over the bound model with the
// base options plus any extra (e.g. hydrated history).
func (a *Agent[T]) newInner(extra ...agent.Option) agent.Agent {
	opts := make([]agent.Option, 0, len(a.baseOpts)+len(extra))
	opts = append(opts, a.baseOpts...)
	opts = append(opts, extra...)
	return agent.New(a.lm, opts...)
}

// WithStore sets the backing store. Without it, [New] uses a fresh
// in-memory store — fine for tests and ephemeral agents; pass a
// persistent store to survive restarts.
func WithStore[T any](s session.Store[T]) agent.Option {
	return agent.WithExtensionMutator(extKey, func(v any) any {
		e := extFrom(v)
		e.store = s
		return e
	})
}

// WithSessionID sets the session ID to create or resume. Without it,
// [New] generates one, readable via [Agent.Session].
func WithSessionID(id string) agent.Option {
	return agent.WithExtensionMutator(extKey, func(v any) any {
		e := extFrom(v)
		e.sessionID = id
		return e
	})
}

// WithPublisher sets the [Publisher] that receives the session's
// events. A forked agent inherits it.
func WithPublisher(p Publisher) agent.Option {
	return agent.WithExtensionMutator(extKey, func(v any) any {
		e := extFrom(v)
		e.publisher = p
		return e
	})
}

// publish delivers a session event to the publisher, if any. Callers
// must not hold a.mu — Publish runs application code that may call
// back into the agent.
func (a *Agent[T]) publish(evt Event) {
	if a.publisher != nil {
		a.publisher.Publish(evt)
	}
}

// New returns a durable [Agent], creating its session if it does not
// exist and hydrating history from the store otherwise. The resume
// point is the last appended entry. Publishes session_init to the
// [WithPublisher] publisher.
//
// Everything but the factory is optional: without [WithStore] the
// session lives in a fresh in-memory store, and without
// [WithSessionID] a session ID is generated.
//
// New does not track instances — each call returns a fresh one, and
// the caller owns instance discipline. Two live instances of the same
// session cannot corrupt it: each appends from its own leaf, so
// concurrent instances grow sibling branches in the tree.
func New[T any](
	ctx context.Context,
	lm ai.LanguageModel,
	opts ...agent.Option,
) (*Agent[T], error) {
	dcfg := extFrom(agent.ApplyOptions(opts...).Extensions[extKey])

	var store session.Store[T]
	switch s := dcfg.store.(type) {
	case nil:
		store = session.NewMemoryStore[T]()
	case session.Store[T]:
		store = s
	default:
		return nil, fmt.Errorf("durable: store is %T, want session.Store[%T]", dcfg.store, *new(T))
	}

	sessionID := dcfg.sessionID
	if sessionID == "" {
		sessionID = newEntryID()
	}

	sess, entries, err := store.LoadSession(ctx, sessionID)
	switch {
	case errors.Is(err, session.ErrSessionNotFound):
		now := time.Now()
		sess = &session.Session[T]{
			ID:        sessionID,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if cerr := store.CreateSession(ctx, sess); cerr != nil {
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

	a := &Agent[T]{
		store:     store,
		lm:        lm,
		baseOpts:  opts,
		publisher: dcfg.publisher,
		sess:      sess,
		entries:   entries,
		index:     index,
		leafID:    leaf,
	}
	a.publish(Event{
		Type:      EventSessionInit,
		SessionID: sessionID,
		LeafID:    leaf,
	})
	return a, nil
}

// Run persists msgs as entries at the leaf, then executes the inner
// loop, persisting each message it produces before forwarding its
// message_end. Zero msgs continues from the current leaf.
//
// The stream is turn-scoped: inner agent events lifted under
// [EventAgent], with persistence receipts on the boundary events.
// Session events go to the [WithPublisher] publisher. A persist
// failure fails the run loudly.
func (a *Agent[T]) Run(ctx context.Context, msgs ...ai.Message) *Stream {
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

		// Persist input before the run starts.
		var inputEntries []session.Entry
		if len(msgs) > 0 {
			entries := make([]session.Entry, len(msgs))
			for i, m := range msgs {
				entries[i] = session.NewMessageEntry(m)
			}
			var err error
			inputEntries, err = a.persist(ctx, entries...)
			if err != nil {
				return nil, fmt.Errorf("durable: persist input: %w", err)
			}
		}

		a.mu.Lock()
		history := a.modelViewLocked()
		a.mu.Unlock()

		inner := a.newInner(agent.WithHistory(history...))
		defer inner.Close()

		s := inner.Run(ctx)
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

// Messages returns the model view of the active path — root to leaf,
// compaction-aware, meta entries included, custom entries excluded.
// Dangling tool calls (a crash between an assistant message and its
// tool results) are repaired with synthesized interrupted results.
func (a *Agent[T]) Messages() []ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.modelViewLocked()
}

// Close marks the instance closed. A closed agent rejects further
// runs; call [New] with the same session ID to resume.
func (a *Agent[T]) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// --- durable verbs ---

// Session returns a copy of the session this instance is bound to,
// including its current [session.Session.State].
func (a *Agent[T]) Session() *session.Session[T] {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := *a.sess
	return &cp
}

// SetState records a session-state change by appending a
// [session.StateEntry] to the log. State is last-wins and
// position-independent — the fold of the log (see [session.LatestState]),
// not reverted by [Agent.Branch]. Publishes session_updated.
func (a *Agent[T]) SetState(ctx context.Context, state T) error {
	a.mu.Lock()
	entries, err := a.persistLocked(ctx, session.NewStateEntry(state))
	if err != nil {
		a.mu.Unlock()
		return err
	}
	a.sess.State = state
	leaf := a.leafID
	a.mu.Unlock()

	a.publish(Event{
		Type:    EventSessionUpdated,
		Entries: entries,
		LeafID:  leaf,
	})
	return nil
}

// LeafID returns the ID of the entry the leaf pointer is on, or
// empty for a fresh session. Capture it before a risky turn and pass
// it to [Agent.Branch] to rewind — a checkpoint is just a remembered
// leaf.
func (a *Agent[T]) LeafID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.leafID
}

// Entries returns the full persisted log in append order, including
// meta and custom entries. Use [session.Tree] to derive the tree, or
// [Agent.Transcript] for the display view of the active path.
func (a *Agent[T]) Entries(ctx context.Context) ([]session.Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]session.Entry, len(a.entries))
	copy(out, a.entries)
	return out, nil
}

// Transcript returns the active path for display: root to leaf, meta
// entries hidden, custom entries included.
func (a *Agent[T]) Transcript(ctx context.Context) ([]session.Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	path := session.PathFrom(a.index, a.leafID)
	return session.TranscriptView(path), nil
}

// Append persists custom entries at the leaf without running the
// loop. Custom entries are not sent to the model.
func (a *Agent[T]) Append(ctx context.Context, entries ...session.Entry) error {
	_, err := a.persist(ctx, entries...)
	return err
}

// Branch moves the leaf pointer to an earlier entry. Zero-copy and
// in-session: the next turn grows a sibling branch, and the abandoned
// branch stays in the tree. Edit, retry, and rewind are all this.
// Publishes session_branched.
//
// The leaf position is in-memory; reopening a session resumes at the
// last appended entry.
func (a *Agent[T]) Branch(ctx context.Context, entryID string) error {
	a.mu.Lock()

	if _, ok := a.index[entryID]; !ok {
		a.mu.Unlock()
		return fmt.Errorf("durable: entry %q not found", entryID)
	}

	from := a.leafID
	a.leafID = entryID
	a.mu.Unlock()

	a.publish(Event{
		Type:   EventSessionBranched,
		FromID: from,
		LeafID: entryID,
	})
	return nil
}

// Fork copies the active path into a new session with the given ID
// and returns a fresh [Agent] bound to it. The new session records
// this one as [session.Session.ParentID]. Returns
// [session.ErrSessionExists] if the ID is taken.
//
// The child inherits this agent's publisher. A successful fork
// publishes session_forked for the source, then session_init for the
// child.
func (a *Agent[T]) Fork(ctx context.Context, newID string) (*Agent[T], error) {
	a.mu.Lock()
	path := session.PathFrom(a.index, a.leafID)
	now := time.Now()
	newSess := &session.Session[T]{
		ID:        newID,
		ParentID:  a.sess.ID,
		CreatedAt: now,
		UpdatedAt: now,
		State:     a.sess.State,
	}
	a.mu.Unlock()

	if err := a.store.CreateSession(ctx, newSess); err != nil {
		return nil, err
	}

	// Re-chain the path with fresh IDs; timestamps carry over.
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

	child := &Agent[T]{
		store:     a.store,
		lm:        a.lm,
		baseOpts:  a.baseOpts,
		publisher: a.publisher,
		sess:      newSess,
		entries:   copied,
		index:     index,
		leafID:    parent,
	}

	a.publish(Event{
		Type:      EventSessionForked,
		SessionID: newID,
		ParentID:  a.sess.ID,
	})
	child.publish(Event{
		Type:      EventSessionInit,
		SessionID: newID,
		LeafID:    parent,
	})
	return child, nil
}

// defaultCompactPrompt instructs the summarizer when no [CompactPrompt]
// override is supplied.
const defaultCompactPrompt = "Summarize the conversation so far. Preserve every fact, decision, and open question in a compact form."

// CompactOption configures a [Agent.Compact] call.
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

// CompactPrompt overrides the instruction given to the summarizer agent.
// Defaults to [defaultCompactPrompt].
func CompactPrompt(s string) CompactOption {
	return func(c *compactConfig) { c.prompt = s }
}

// Compact appends a [session.CompactionEntry] summarizing older turns
// on the active path. Nothing is deleted — the full tree stays
// rewindable. The summary is written by an ephemeral agent from the
// session's own factory; custom entries pass through untouched.
// Publishes session_compacted.
func (a *Agent[T]) Compact(ctx context.Context, opts ...CompactOption) error {
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

	summarizer := a.newInner(agent.WithHistory(repairToolCalls(toSummarize)...))
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
	leaf := a.leafID
	a.mu.Unlock()

	a.publish(Event{
		Type:    EventSessionCompacted,
		Entries: entries,
		LeafID:  leaf,
	})
	return nil
}

// --- internals ---

// persist assigns tree headers to entries, chains them at the leaf,
// appends them to the store, and advances the leaf.
func (a *Agent[T]) persist(ctx context.Context, entries ...session.Entry) ([]session.Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.persistLocked(ctx, entries...)
}

func (a *Agent[T]) persistLocked(ctx context.Context, entries ...session.Entry) ([]session.Entry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	now := time.Now()
	parent := a.leafID
	out := make([]session.Entry, len(entries))
	for i, e := range entries {
		h := session.EntryHeader{
			ID:        newEntryID(),
			ParentID:  parent,
			CreatedAt: now,
		}
		out[i] = withHeader(e, h)
		parent = h.ID
	}

	if err := a.store.AppendEntries(ctx, a.sess.ID, out...); err != nil {
		return nil, err
	}

	a.entries = append(a.entries, out...)
	for _, e := range out {
		a.index[e.Header().ID] = e
	}
	a.leafID = parent
	a.sess.UpdatedAt = now
	return out, nil
}

// modelViewLocked projects the active path for the model, repairing
// dangling tool calls. Callers must hold a.mu.
func (a *Agent[T]) modelViewLocked() []ai.Message {
	path := session.PathFrom(a.index, a.leafID)
	return repairToolCalls(session.ModelView(path))
}

// repairToolCalls synthesizes interrupted tool results for assistant
// tool calls that have none — the footprint of a crash between an
// assistant message persisting and its tool results persisting.
// Providers reject a tool call with no result, so resume repairs it.
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

// withHeader returns a copy of e with its embedded
// [session.EntryHeader] replaced. Custom entries reach the header
// through their embedded [session.CustomEntry].
func withHeader(e session.Entry, h session.EntryHeader) session.Entry {
	v := reflect.New(reflect.TypeOf(e)).Elem()
	v.Set(reflect.ValueOf(e))
	if f := findHeaderField(v); f.IsValid() && f.CanSet() {
		f.Set(reflect.ValueOf(h))
	}
	return v.Interface().(session.Entry)
}

var headerType = reflect.TypeOf(session.EntryHeader{})

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
