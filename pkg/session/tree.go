package session

import "sort"

// Node is one node of the derived transcript tree, for navigation
// and display. Children are sorted by CreatedAt, oldest first.
type Node struct {
	Entry    Entry
	Children []*Node
}

// Tree derives the transcript tree from an append-ordered entry log.
// A well-formed session has exactly one root; entries whose ParentID
// names no known entry are returned as additional roots. Children are
// sorted by CreatedAt, oldest first.
func Tree(entries []Entry) []*Node {
	known := make(map[string]bool, len(entries))
	for _, e := range entries {
		known[e.Header().ID] = true
	}

	childrenOf := make(map[string][]Entry)
	var roots []Entry
	for _, e := range entries {
		parentID := e.Header().ParentID
		if parentID == "" || !known[parentID] {
			roots = append(roots, e)
			continue
		}
		childrenOf[parentID] = append(childrenOf[parentID], e)
	}

	byCreatedAt := func(entries []Entry) {
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].Header().CreatedAt.Before(entries[j].Header().CreatedAt)
		})
	}

	var build func(e Entry) *Node
	build = func(e Entry) *Node {
		kids := childrenOf[e.Header().ID]
		byCreatedAt(kids)
		node := &Node{Entry: e}
		for _, kid := range kids {
			node.Children = append(node.Children, build(kid))
		}
		return node
	}

	byCreatedAt(roots)
	out := make([]*Node, 0, len(roots))
	for _, r := range roots {
		out = append(out, build(r))
	}
	return out
}
