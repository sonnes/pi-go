package session

import "github.com/sonnes/pi-go/pkg/ai"

// Path returns the root→leaf path ending at the entry with ID leafID,
// derived by walking ParentID pointers. It returns nil for an empty or
// unknown leafID. A missing parent stops the walk (the collected suffix
// is returned); a parent cycle terminates rather than looping.
//
// Path builds an index over entries on every call. Callers that append
// incrementally and walk repeatedly (see the durable agent) should keep a
// live index and use [PathFrom] to avoid the per-call O(len(entries))
// rebuild.
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

// PathFrom is [Path] over a caller-maintained index: it walks ParentID
// pointers through byID rather than rebuilding the index each call. The
// walk is O(path depth), not O(total entries). Same semantics as [Path]
// for empty, unknown, orphaned, and cyclic leaves.
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

// ModelView projects a root→leaf path (see [Path]) into the messages
// sent to the model: [MessageEntry] values including meta entries;
// custom and state entries are skipped.
//
// When the path contains [CompactionEntry] values, the latest one wins:
// its summary is emitted as a user message, entries before its
// FirstKeptID are elided, and everything from FirstKeptID onward is
// kept verbatim.
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

// TranscriptView projects a root→leaf path (see [Path]) for display:
// meta message entries are hidden, everything else — including custom
// entries the model never sees — is kept.
func TranscriptView(path []Entry) []Entry {
	var out []Entry
	for _, e := range path {
		if me, ok := e.(MessageEntry); ok && me.Meta {
			continue
		}
		out = append(out, e)
	}
	return out
}
