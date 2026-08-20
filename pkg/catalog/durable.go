package catalog

import (
	"context"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/durable"
)

// DurableAgent builds a session-backed [durable.Agent] for a
// "<kind>/<model>" spec. It resolves the spec through the catalog, like
// [Catalog.Agent]. Then it binds the resulting [ai.LanguageModel] to a
// durable session loop.
//
// Pass the durable options ([durable.WithStore], [durable.WithSessionID])
// together with the agent options.
func (c *Catalog) DurableAgent(
	ctx context.Context,
	spec string,
	opts ...agent.Option,
) (*durable.Agent, error) {
	lm, err := c.LanguageModel(spec)
	if err != nil {
		return nil, err
	}
	return durable.New(ctx, durable.Model(lm), opts...)
}
