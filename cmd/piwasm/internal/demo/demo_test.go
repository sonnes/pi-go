package demo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collect runs a demo with a recording emitter.
func collect(t *testing.T) (*Demo, *[]Event) {
	t.Helper()

	var events []Event
	d, err := New(t.Context(), func(e Event) { events = append(events, e) })
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	return d, &events
}

func kinds(events []Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func latestTree(t *testing.T, events []Event) []Node {
	t.Helper()

	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == KindTree {
			return events[i].Tree
		}
	}
	t.Fatal("no tree event emitted")

	return nil
}

func TestRunStreamsDeltasAndDispatchesTheTool(t *testing.T) {
	d, events := collect(t)

	require.NoError(t, d.Handle(t.Context(), Command{Kind: CmdRun, Text: "What's the weather in Paris?"}))

	assert.Contains(t, kinds(*events), KindDelta, "assistant text should stream")
	assert.Contains(t, kinds(*events), KindTool, "the scripted turn asks for a tool")

	var streamed string
	for _, e := range *events {
		if e.Kind == KindDelta {
			streamed += e.Text
		}
	}
	assert.NotEmpty(t, streamed)
}

func TestRunBuildsTheTranscriptTree(t *testing.T) {
	d, events := collect(t)

	require.NoError(t, d.Handle(t.Context(), Command{Kind: CmdRun, Text: "What's the weather in Paris?"}))

	// user → assistant(tool call) → tool_result → assistant
	var roles []string
	for node := onlyRoot(t, latestTree(t, *events)); node != nil; node = firstChild(node) {
		roles = append(roles, node.Role)
	}

	assert.Equal(t, []string{"user", "assistant", "tool_result", "assistant"}, roles)
}

func TestBranchGrowsASiblingAndKeepsBothPaths(t *testing.T) {
	d, events := collect(t)
	ctx := t.Context()

	require.NoError(t, d.Handle(ctx, Command{Kind: CmdRun, Text: "Weather in Paris?"}))
	root := onlyRoot(t, latestTree(t, *events))

	require.NoError(t, d.Handle(ctx, Command{Kind: CmdBranch, EntryID: root.ID}))
	require.NoError(t, d.Handle(ctx, Command{Kind: CmdRun, Text: "Actually, Rome?"}))

	root = onlyRoot(t, latestTree(t, *events))
	assert.Len(t, root.Children, 2, "the abandoned branch stays in the tree")
}

func TestBranchRejectsAnUnknownEntry(t *testing.T) {
	d, _ := collect(t)

	err := d.Handle(t.Context(), Command{Kind: CmdBranch, EntryID: "nope"})
	assert.Error(t, err)
}

func TestCompactAppendsASummaryWithoutLosingHistory(t *testing.T) {
	d, events := collect(t)
	ctx := t.Context()

	require.NoError(t, d.Handle(ctx, Command{Kind: CmdRun, Text: "Weather in Paris?"}))
	before := countNodes(latestTree(t, *events))

	require.NoError(t, d.Handle(ctx, Command{Kind: CmdCompact}))

	after := countNodes(latestTree(t, *events))
	assert.Greater(t, after, before, "compaction appends, it never deletes")

	assert.True(t, hasRole(latestTree(t, *events), "compaction"))
}

func TestResetClearsTheTree(t *testing.T) {
	d, events := collect(t)
	ctx := t.Context()

	require.NoError(t, d.Handle(ctx, Command{Kind: CmdRun, Text: "Weather in Paris?"}))
	require.NotEmpty(t, latestTree(t, *events))

	require.NoError(t, d.Handle(ctx, Command{Kind: CmdReset}))
	assert.Empty(t, latestTree(t, *events))
}

func TestUnknownCommandIsAnError(t *testing.T) {
	d, _ := collect(t)

	err := d.Handle(t.Context(), Command{Kind: "explode"})
	assert.Error(t, err)
}

func TestLiveModeRequiresAKey(t *testing.T) {
	d, _ := collect(t)

	err := d.Handle(t.Context(), Command{Kind: CmdKey, Key: ""})
	assert.Error(t, err)
}

// --- helpers over the emitted tree ---

func onlyRoot(t *testing.T, tree []Node) *Node {
	t.Helper()
	require.Len(t, tree, 1, "a well-formed session has one root")

	return &tree[0]
}

func firstChild(n *Node) *Node {
	if len(n.Children) == 0 {
		return nil
	}

	return &n.Children[0]
}

func countNodes(tree []Node) int {
	total := 0
	for _, n := range tree {
		total += 1 + countNodes(n.Children)
	}

	return total
}

func hasRole(tree []Node, role string) bool {
	for _, n := range tree {
		if n.Role == role || hasRole(n.Children, role) {
			return true
		}
	}

	return false
}
