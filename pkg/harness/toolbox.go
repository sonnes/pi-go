package harness

import (
	"fmt"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Reserved tool names. The harness synthesizes "skill" from the
// resolved skills. "agent" stays reserved so the subagent tool can
// return without breaking anyone. A user tool cannot claim either name.
const (
	skillToolName = "skill"
	agentToolName = "agent"
)

// toolbox is the tool registry of the harness. It holds a lookup by
// name and the registration order, which is the order the model sees.
type toolbox struct {
	order  []string
	byName map[string]ai.Tool
}

// newToolbox indexes tools by name and rejects the reserved synthesized
// names. A name registered again replaces the earlier tool in place.
// Artifacts get the same last-wins, position-stable semantics. A
// per-build tool can therefore override a baseline tool, and the list
// the model sees keeps its order.
func newToolbox(tools []ai.Tool) (toolbox, error) {
	b := toolbox{byName: make(map[string]ai.Tool, len(tools))}
	for _, t := range tools {
		name := t.Info().Name
		switch name {
		case skillToolName, agentToolName:
			return toolbox{}, fmt.Errorf(
				"harness: tool name %q is reserved for the synthesized tool",
				name,
			)
		}
		if _, ok := b.byName[name]; !ok {
			b.order = append(b.order, name)
		}
		b.byName[name] = t
	}
	return b, nil
}

// list returns every tool in registration order.
func (b toolbox) list() []ai.Tool {
	out := make([]ai.Tool, 0, len(b.order))
	for _, name := range b.order {
		out = append(out, b.byName[name])
	}
	return out
}
