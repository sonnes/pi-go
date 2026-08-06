package durable_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/session"
)

// record returns middleware that appends name to order when it runs.
func record(order *[]string, name string) durable.Middleware {
	return func(next durable.Runner) durable.Runner {
		return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
			*order = append(*order, name)
			return next(ctx, entries...)
		}
	}
}

// promptText joins every message in a prompt, so a test can ask what
// the model saw without caring where in the history it landed.
func promptText(p ai.Prompt) string {
	var b strings.Builder
	for _, m := range p.Messages {
		b.WriteString(m.Text())
		b.WriteString("\n")
	}
	return b.String()
}

func TestMiddlewareOrder(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}
	var order []string

	da, err := durable.New(
		t.Context(),
		testLM(prov),
		durable.WithMiddleware(record(&order, "first")),
		durable.WithMiddleware(record(&order, "second"), record(&order, "third")),
	)
	require.NoError(t, err)
	defer da.Close()

	_, err = da.Run(t.Context(), durable.Text("hi")).Wait()
	require.NoError(t, err)

	assert.Equal(t, []string{"first", "second", "third"}, order,
		"WithMiddleware accumulates, and the first registered is the outermost")
}

func TestMiddlewareWrapsRun(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}

	inject := func(next durable.Runner) durable.Runner {
		return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
			note := durable.Ephemeral(durable.Text("REMINDER"))
			return next(ctx, append([]session.Entry{note}, entries...)...)
		}
	}

	da, err := durable.New(t.Context(), testLM(prov), durable.WithMiddleware(inject))
	require.NoError(t, err)
	defer da.Close()

	_, err = da.Run(t.Context(), durable.Text("hi")).Wait()
	require.NoError(t, err)

	assert.Contains(t, promptText(prov.prompt(0)), "REMINDER")
}

func TestMiddlewareRunsOnTheCallersGoroutine(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{textStream("ok")}}

	entered := false
	mark := func(next durable.Runner) durable.Runner {
		return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
			entered = true
			return next(ctx, entries...)
		}
	}

	da, err := durable.New(t.Context(), testLM(prov), durable.WithMiddleware(mark))
	require.NoError(t, err)
	defer da.Close()

	// The chain is synchronous: by the time Run has handed back a
	// stream, every middleware has already seen the call. Nothing in the
	// chain executes inside the producer goroutine.
	s := da.Run(t.Context(), durable.Text("hi"))
	assert.True(t, entered, "middleware ran before Run returned")

	_, err = s.Wait()
	require.NoError(t, err)
}

func TestMiddlewareStateIsPerAgent(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("a"),
		textStream("b"),
	}}

	// Closure state created when the middleware is applied — once per
	// agent instance, so two agents cannot share it.
	once := func(next durable.Runner) durable.Runner {
		fired := false
		return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
			if !fired {
				fired = true
				note := durable.Ephemeral(durable.Text("FIRST"))
				entries = append([]session.Entry{note}, entries...)
			}
			return next(ctx, entries...)
		}
	}

	opts := []agent.Option{durable.WithMiddleware(once)}

	a1, err := durable.New(t.Context(), testLM(prov), opts...)
	require.NoError(t, err)
	defer a1.Close()
	_, err = a1.Run(t.Context(), durable.Text("hi")).Wait()
	require.NoError(t, err)

	a2, err := durable.New(t.Context(), testLM(prov), opts...)
	require.NoError(t, err)
	defer a2.Close()
	_, err = a2.Run(t.Context(), durable.Text("hi")).Wait()
	require.NoError(t, err)

	assert.Contains(t, promptText(prov.prompt(0)), "FIRST")
	assert.Contains(t, promptText(prov.prompt(1)), "FIRST", "a fresh agent gets fresh state")
}

func TestMiddlewareCanRefuseARun(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{textStream("never")}}
	denied := assert.AnError

	deny := func(durable.Runner) durable.Runner {
		return func(context.Context, ...session.Entry) *durable.Stream {
			return durable.Fail(denied)
		}
	}

	da, err := durable.New(t.Context(), testLM(prov), durable.WithMiddleware(deny))
	require.NoError(t, err)
	defer da.Close()

	msgs, err := da.Run(t.Context(), durable.Text("hi")).Wait()
	require.Error(t, err)
	assert.ErrorIs(t, err, denied)
	assert.Nil(t, msgs)

	// Refusing means refusing: the model was never called and nothing
	// was persisted.
	assert.Equal(t, 0, prov.promptCount())
	entries, err := da.Entries(t.Context())
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestForkCarriesMiddlewareWithFreshClosures(t *testing.T) {
	prov := &mockProvider{responses: []*ai.EventStream{
		textStream("one"),
		textStream("two"),
		textStream("three"),
	}}

	// Each instantiation of this middleware counts its own runs, so the
	// tag the model sees says which instance handled the run.
	counting := func(next durable.Runner) durable.Runner {
		n := 0
		return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
			n++
			note := durable.Ephemeral(durable.Text(tag(n)))
			return next(ctx, append([]session.Entry{note}, entries...)...)
		}
	}

	store := session.NewMemoryStore()
	parent, err := durable.New(
		t.Context(),
		testLM(prov),
		durable.WithStore(store),
		durable.WithSessionID("p1"),
		durable.WithMiddleware(counting),
	)
	require.NoError(t, err)
	defer parent.Close()

	_, err = parent.Run(t.Context(), durable.Text("hi")).Wait()
	require.NoError(t, err)
	assert.Contains(t, promptText(prov.prompt(0)), "COUNT=1")

	child, err := parent.Fork(t.Context(), "c1")
	require.NoError(t, err)
	defer child.Close()

	// The fork runs the parent's middleware — it used to silently lose
	// it, because only the wrapper's Run was overridden.
	_, err = child.Run(t.Context(), durable.Text("hi")).Wait()
	require.NoError(t, err)
	assert.Contains(t, promptText(prov.prompt(1)), "COUNT=1",
		"the child's chain is freshly instantiated, so its counter starts over")

	// And the two counters are genuinely separate.
	_, err = parent.Run(t.Context(), durable.Text("again")).Wait()
	require.NoError(t, err)
	assert.Contains(t, promptText(prov.prompt(2)), "COUNT=2",
		"the parent's own state advanced independently")
}

func tag(n int) string {
	return "COUNT=" + string(rune('0'+n))
}
