package catalog

import (
	"context"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/durable"
)

// DurableAgent builds a session-backed [durable.Agent] for a
// "<kind>/<model>" spec. It resolves the spec through the catalog like
// [Catalog.Agent], then binds the resulting [ai.LanguageModel] to a
// durable session loop.
//
// Pass durable options ([durable.WithStore], [durable.WithSessionID])
// alongside agent options.
func (c *Catalog) DurableAgent(
	ctx context.Context,
	spec string,
	opts ...agent.Option,
) (*durable.Agent, error) {
	lm, err := c.LanguageModel(spec)
	if err != nil {
		return nil, err
	}
	return durable.New(ctx, lm, opts...)
}
