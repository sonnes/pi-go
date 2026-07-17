package claude

import (
	"fmt"
	"strings"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Factory returns an [agent.Factory] that builds a Claude CLI agent from
// a "<kind>/<model>" spec, e.g. "claude/sonnet". Register it with a
// catalog under the kind prefix. base options apply to every built
// agent, before any per-call options.
func Factory(base ...agent.Option) agent.Factory {
	return func(spec string, opts ...agent.Option) (agent.Agent, error) {
		_, model, ok := strings.Cut(spec, "/")
		if !ok || model == "" {
			return nil, fmt.Errorf("claude: invalid agent spec %q: want \"<kind>/<model>\"", spec)
		}
		all := append(append([]agent.Option(nil), base...), opts...)
		return New(ai.Model{ID: model, Name: model}, all...), nil
	}
}
