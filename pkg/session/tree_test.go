package session_test

import (
	"testing"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func treeMsg(id, parent string, createdAt time.Time) session.MessageEntry {
	return session.MessageEntry{
		EntryHeader: session.EntryHeader{ID: id, ParentID: parent, CreatedAt: createdAt},
		Message:     ai.UserMessage(id),
	}
}

func TestTree(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []session.Entry{
		treeMsg("e1", "", base),
		treeMsg("e2", "e1", base.Add(1*time.Second)),
		treeMsg("e3", "e1", base.Add(2*time.Second)),
		treeMsg("e4", "e2", base.Add(3*time.Second)),
	}

	roots := session.Tree(entries)
	require.Len(t, roots, 1)
	assert.Equal(t, "e1", roots[0].Entry.Header().ID)

	require.Len(t, roots[0].Children, 2)
	assert.Equal(t, "e2", roots[0].Children[0].Entry.Header().ID)
	assert.Equal(t, "e3", roots[0].Children[1].Entry.Header().ID)

	require.Len(t, roots[0].Children[0].Children, 1)
	assert.Equal(t, "e4", roots[0].Children[0].Children[0].Entry.Header().ID)
}

func TestTreeOrphanBecomesRoot(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []session.Entry{
		treeMsg("e1", "", base),
		treeMsg("e2", "missing", base.Add(1*time.Second)),
	}
	roots := session.Tree(entries)
	assert.Len(t, roots, 2)
}

func TestTreeChildrenSortedByCreatedAt(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Appended out of timestamp order; Tree must sort children by CreatedAt.
	entries := []session.Entry{
		treeMsg("e1", "", base),
		treeMsg("late", "e1", base.Add(5*time.Second)),
		treeMsg("early", "e1", base.Add(1*time.Second)),
	}
	roots := session.Tree(entries)
	require.Len(t, roots[0].Children, 2)
	assert.Equal(t, "early", roots[0].Children[0].Entry.Header().ID)
	assert.Equal(t, "late", roots[0].Children[1].Entry.Header().ID)
}

func TestTreeEmpty(t *testing.T) {
	assert.Empty(t, session.Tree(nil))
}
