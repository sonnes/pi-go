package harness

import (
	"fmt"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Reserved tool names. The harness synthesizes "skill" from the
// resolved skills; "agent" stays reserved so the subagent tool can
// return without breaking anyone. A user tool cannot claim either.
const (
	skillToolName = "skill"
	agentToolName = "agent"
)

// toolbox is the harness's tool registry: name lookup plus the
// registration order, which is the order the model sees them in.
type toolbox struct {
	order  []string
	byName map[string]ai.Tool
}

// newToolbox indexes tools by name, rejecting the reserved synthesized
// names. Registering a name again replaces the earlier tool in place —
// the same last-wins, position-stable semantics artifacts get — so a
// per-build tool can override a baseline one without reshuffling the
// list the model sees.
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
