package codex

import (
	"fmt"
	"strings"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Factory returns an [agent.Factory] that builds a Codex CLI agent from
// a "<kind>/<model>" spec, for example "codex/gpt-5". Register it with a
// catalog under the kind prefix. The base options apply to every agent
// that the factory builds. The per-call options apply after them.
func Factory(base ...agent.Option) agent.Factory {
	return func(spec string, opts ...agent.Option) (agent.Agent, error) {
		_, model, ok := strings.Cut(spec, "/")
		if !ok || model == "" {
			return nil, fmt.Errorf("codex: invalid agent spec %q: want \"<kind>/<model>\"", spec)
		}
		all := append(append([]agent.Option(nil), base...), opts...)
		return New(ai.Model{ID: model, Name: model}, all...), nil
	}
}
