package catalog

import (
	"context"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/durable"
)

// DurableAgent builds a session-backed [durable.Agent] for a "<kind>/<model>"
// spec. It resolves the spec through the catalog like [Catalog.Agent], then
// binds the resulting [ai.LanguageModel] to a durable session loop. Like
// [GenerateObject], it is a function rather than a method because Go methods
// cannot be generic; T is the session state type.
//
// Pass durable options ([durable.WithStore], [durable.WithSessionID],
// [durable.WithPublisher]) alongside agent options.
func DurableAgent[T any](
	ctx context.Context,
	c *Catalog,
	spec string,
	opts ...agent.Option,
) (*durable.Agent[T], error) {
	lm, err := c.LanguageModel(spec)
	if err != nil {
		return nil, err
	}
	return durable.New[T](ctx, lm, opts...)
}
