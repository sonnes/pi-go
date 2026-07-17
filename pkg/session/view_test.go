package session_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
)

// viewEntry is a test custom entry for projection tests.
type viewEntry struct {
	session.CustomEntry
	Note string
}

func header(id, parent string, at int) session.EntryHeader {
	return session.EntryHeader{
		ID:        id,
		ParentID:  parent,
		CreatedAt: time.Unix(int64(at), 0),
	}
}

func msgEntry(id, parent string, at int, msg ai.Message) session.MessageEntry {
	return session.MessageEntry{
		EntryHeader: header(id, parent, at),
		Message:     msg,
	}
}

func metaEntry(id, parent string, at int, msg ai.Message) session.MessageEntry {
	e := msgEntry(id, parent, at, msg)
	e.Meta = true
	return e
}

func TestPath(t *testing.T) {
	e1 := msgEntry("e1", "", 1, ai.UserMessage("one"))
	e2 := msgEntry("e2", "e1", 2, ai.AssistantMessage(ai.Text{Text: "two"}))
	e3 := msgEntry("e3", "e2", 3, ai.UserMessage("three"))
	e5 := msgEntry("e5", "e1", 5, ai.UserMessage("five"))

	tests := []struct {
		name    string
		entries []session.Entry
		leafID  string
		wantIDs []string
	}{
		{
			name:    "empty leaf",
			entries: []session.Entry{e1, e2},
			leafID:  "",
			wantIDs: nil,
		},
		{
			name:    "linear",
			entries: []session.Entry{e1, e2, e3},
			leafID:  "e3",
			wantIDs: []string{"e1", "e2", "e3"},
		},
		{
			name:    "leaf picks its branch",
			entries: []session.Entry{e1, e2, e3, e5},
			leafID:  "e5",
			wantIDs: []string{"e1", "e5"},
		},
		{
			name:    "mid-path leaf",
			entries: []session.Entry{e1, e2, e3},
			leafID:  "e2",
			wantIDs: []string{"e1", "e2"},
		},
		{
			name:    "unknown leaf",
			entries: []session.Entry{e1, e2},
			leafID:  "nope",
			wantIDs: nil,
		},
		{
			name: "orphaned parent stops the walk",
			entries: []session.Entry{
				msgEntry("x2", "missing", 2, ai.UserMessage("x")),
				msgEntry("x3", "x2", 3, ai.UserMessage("y")),
			},
			leafID:  "x3",
			wantIDs: []string{"x2", "x3"},
		},
		{
			name: "cycle terminates",
			entries: []session.Entry{
				msgEntry("c1", "c2", 1, ai.UserMessage("a")),
				msgEntry("c2", "c1", 2, ai.UserMessage("b")),
			},
			leafID:  "c2",
			wantIDs: []string{"c1", "c2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := session.Path(tt.entries, tt.leafID)
			var ids []string
			for _, e := range got {
				ids = append(ids, e.Header().ID)
			}
			assert.Equal(t, tt.wantIDs, ids)
		})
	}
}

func TestPathFrom(t *testing.T) {
	e1 := msgEntry("e1", "", 1, ai.UserMessage("one"))
	e2 := msgEntry("e2", "e1", 2, ai.AssistantMessage(ai.Text{Text: "two"}))
	e3 := msgEntry("e3", "e2", 3, ai.UserMessage("three"))
	e5 := msgEntry("e5", "e1", 5, ai.UserMessage("five"))
	entries := []session.Entry{e1, e2, e3, e5}

	byID := make(map[string]session.Entry, len(entries))
	for _, e := range entries {
		byID[e.Header().ID] = e
	}

	// PathFrom over a prebuilt index matches Path over the slice.
	for _, leaf := range []string{"", "e3", "e5", "e2", "nope"} {
		want := session.Path(entries, leaf)
		got := session.PathFrom(byID, leaf)
		assert.Equal(t, want, got, "leaf %q", leaf)
	}
}

func benchEntries(n int) ([]session.Entry, map[string]session.Entry, string) {
	entries := make([]session.Entry, n)
	byID := make(map[string]session.Entry, n)
	parent := ""
	for i := range entries {
		id := "e" + strconv.Itoa(i)
		e := msgEntry(id, parent, i, ai.UserMessage(id))
		entries[i] = e
		byID[id] = e
		parent = id
	}
	return entries, byID, parent
}

// Path rebuilds the index every call (O(n)); PathFrom reuses a live one
// (O(depth)). This gap is what makes a long durable session — one Path per
// turn over an ever-growing log — quadratic. The leaf is a shallow entry:
// the rewind case, where the active path is short but the log is huge
// because abandoned branches are never pruned.
func BenchmarkPath(b *testing.B) {
	entries, _, _ := benchEntries(5000)
	leaf := entries[10].Header().ID
	b.ResetTimer()
	for range b.N {
		session.Path(entries, leaf)
	}
}

func BenchmarkPathFrom(b *testing.B) {
	entries, byID, _ := benchEntries(5000)
	leaf := entries[10].Header().ID
	b.ResetTimer()
	for range b.N {
		session.PathFrom(byID, leaf)
	}
}

func TestModelView(t *testing.T) {
	t.Run("messages in path order, meta included", func(t *testing.T) {
		path := []session.Entry{
			msgEntry("e1", "", 1, ai.UserMessage("hi")),
			metaEntry("e2", "e1", 2, ai.UserMessage("attached file")),
			msgEntry("e3", "e2", 3, ai.AssistantMessage(ai.Text{Text: "hello"})),
		}

		msgs := session.ModelView(path)

		require.Len(t, msgs, 3)
		assert.Equal(t, "hi", msgs[0].Text())
		assert.Equal(t, "attached file", msgs[1].Text())
		assert.Equal(t, "hello", msgs[2].Text())
	})

	t.Run("custom and state entries are skipped", func(t *testing.T) {
		path := []session.Entry{
			msgEntry("e1", "", 1, ai.UserMessage("hi")),
			viewEntry{
				CustomEntry: session.CustomEntry{EntryHeader: header("e2", "e1", 2), Kind: "note"},
				Note:        "invisible",
			},
			session.StateEntry[string]{EntryHeader: header("e3", "e2", 3), State: "title"},
			msgEntry("e4", "e3", 4, ai.AssistantMessage(ai.Text{Text: "hello"})),
		}

		msgs := session.ModelView(path)

		require.Len(t, msgs, 2)
		assert.Equal(t, "hi", msgs[0].Text())
		assert.Equal(t, "hello", msgs[1].Text())
	})

	t.Run("compaction elides prefix, keeps tail", func(t *testing.T) {
		path := []session.Entry{
			msgEntry("u1", "", 1, ai.UserMessage("old question")),
			msgEntry("a1", "u1", 2, ai.AssistantMessage(ai.Text{Text: "old answer"})),
			msgEntry("u2", "a1", 3, ai.UserMessage("recent question")),
			msgEntry("a2", "u2", 4, ai.AssistantMessage(ai.Text{Text: "recent answer"})),
			session.CompactionEntry{
				EntryHeader: header("c1", "a2", 5),
				Summary:     "they talked about old things",
				FirstKeptID: "u2",
			},
			msgEntry("u3", "c1", 6, ai.UserMessage("new question")),
		}

		msgs := session.ModelView(path)

		require.Len(t, msgs, 4)
		assert.Equal(t, "they talked about old things", msgs[0].Text())
		assert.Equal(t, ai.RoleUser, msgs[0].Role)
		assert.Equal(t, "recent question", msgs[1].Text())
		assert.Equal(t, "recent answer", msgs[2].Text())
		assert.Equal(t, "new question", msgs[3].Text())
	})

	t.Run("unknown FirstKeptID keeps nothing before the compaction", func(t *testing.T) {
		path := []session.Entry{
			msgEntry("u1", "", 1, ai.UserMessage("old")),
			session.CompactionEntry{
				EntryHeader: header("c1", "u1", 2),
				Summary:     "summary",
				FirstKeptID: "missing",
			},
			msgEntry("u2", "c1", 3, ai.UserMessage("new")),
		}

		msgs := session.ModelView(path)

		require.Len(t, msgs, 2)
		assert.Equal(t, "summary", msgs[0].Text())
		assert.Equal(t, "new", msgs[1].Text())
	})

	t.Run("last compaction wins", func(t *testing.T) {
		path := []session.Entry{
			msgEntry("u1", "", 1, ai.UserMessage("one")),
			session.CompactionEntry{
				EntryHeader: header("c1", "u1", 2),
				Summary:     "first summary",
				FirstKeptID: "u1",
			},
			msgEntry("u2", "c1", 3, ai.UserMessage("two")),
			session.CompactionEntry{
				EntryHeader: header("c2", "u2", 4),
				Summary:     "second summary",
				FirstKeptID: "u2",
			},
			msgEntry("u3", "c2", 5, ai.UserMessage("three")),
		}

		msgs := session.ModelView(path)

		require.Len(t, msgs, 3)
		assert.Equal(t, "second summary", msgs[0].Text())
		assert.Equal(t, "two", msgs[1].Text())
		assert.Equal(t, "three", msgs[2].Text())
	})
}

func TestTranscriptView(t *testing.T) {
	custom := viewEntry{
		CustomEntry: session.CustomEntry{EntryHeader: header("e3", "e2", 3), Kind: "note"},
		Note:        "visible",
	}
	path := []session.Entry{
		msgEntry("e1", "", 1, ai.UserMessage("hi")),
		metaEntry("e2", "e1", 2, ai.UserMessage("attached file")),
		custom,
		msgEntry("e4", "e3", 4, ai.AssistantMessage(ai.Text{Text: "hello"})),
	}

	got := session.TranscriptView(path)

	require.Len(t, got, 3)
	assert.Equal(t, "e1", got[0].Header().ID)
	assert.Equal(t, "e3", got[1].Header().ID)
	assert.Equal(t, "e4", got[2].Header().ID)
}
