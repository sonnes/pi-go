package session

import "github.com/sonnes/pi-go/pkg/ai"

// Path returns the root→leaf path that ends at the entry with ID leafID.
// It walks the ParentID pointers to derive the path. For an empty or
// unknown leafID, it returns nil. A missing parent stops the walk, and
// Path returns the collected suffix. A parent cycle also stops the walk
// instead of looping.
//
// Path builds an index over the entries on every call. [PathFrom] takes a
// caller-maintained index instead. Callers that append incrementally and
// walk repeatedly, like the durable agent, use [PathFrom] to avoid the
// O(len(entries)) rebuild on every call.
func Path(entries []Entry, leafID string) []Entry {
	if leafID == "" {
		return nil
	}

	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byID[e.Header().ID] = e
	}
	return PathFrom(byID, leafID)
}

// PathFrom is [Path] over a caller-maintained index. It walks the
// ParentID pointers through byID and does not rebuild the index on every
// call. The walk is O(path depth), not O(total entries). For empty,
// unknown, orphaned, and cyclic leaves, PathFrom behaves like [Path].
func PathFrom(byID map[string]Entry, leafID string) []Entry {
	if leafID == "" {
		return nil
	}

	var rev []Entry
	seen := make(map[string]bool)
	for id := leafID; id != ""; {
		e, ok := byID[id]
		if !ok || seen[id] {
			break
		}
		seen[id] = true
		rev = append(rev, e)
		id = e.Header().ParentID
	}

	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// ModelView projects a root→leaf path (see [Path]) into the messages for
// the model. The result holds the [MessageEntry] values, meta entries
// included. ModelView skips custom entries.
//
// If the path contains [CompactionEntry] values, the latest one wins.
// ModelView emits its summary as a user message. It omits the entries
// before FirstKeptID. It keeps everything from FirstKeptID onward
// verbatim.
func ModelView(path []Entry) []ai.Message {
	lastComp := -1
	for i, e := range path {
		if _, ok := e.(CompactionEntry); ok {
			lastComp = i
		}
	}

	start := 0
	var out []ai.Message
	if lastComp >= 0 {
		comp := path[lastComp].(CompactionEntry)
		out = append(out, ai.UserMessage(comp.Summary))
		start = lastComp + 1
		for i := range lastComp {
			if path[i].Header().ID == comp.FirstKeptID {
				start = i
				break
			}
		}
	}

	for i := start; i < len(path); i++ {
		if me, ok := path[i].(MessageEntry); ok {
			out = append(out, me.Message)
		}
	}
	return out
}

// TranscriptView projects a root→leaf path (see [Path]) into the entries
// to show a reader. It hides injected context and keeps everything else,
// including the custom entries that the model never sees. Injected
// context is a [MessageEntry] that the model reads and the reader must
// not. It comes in two flavors: Meta, which persists, and Ephemeral,
// which does not.
func TranscriptView(path []Entry) []Entry {
	var out []Entry
	for _, e := range path {
		if me, ok := e.(MessageEntry); ok && (me.Meta || me.Ephemeral) {
			continue
		}
		out = append(out, e)
	}
	return out
}
