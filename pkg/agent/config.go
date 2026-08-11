package agent

import "github.com/sonnes/pi-go/pkg/ai"

// Config is a snapshot of the configuration that a set of [Option] values
// applies. It is the exported view of the internal config type. A
// sub-package uses it to read the options without an import of unexported
// types.
//
// Extensions holds the values that [WithExtension] and
// [WithExtensionMutator] set. The key is the owning sub-package. By
// convention the key is the name of that package.
type Config struct {
	Tools        []ai.Tool
	History      []ai.Message
	SystemPrompt string
	StreamOpts   []ai.Option
	MaxTurns     int
	Hooks        map[HookEvent][]Hook
	Extensions   map[string]any
}

// ApplyOptions applies opts and returns the resulting [Config] snapshot.
// The model goes to [New] as a separate argument, not through an option.
func ApplyOptions(opts ...Option) Config {
	c := config{}
	for _, opt := range opts {
		opt(&c)
	}
	return Config{
		Tools:        c.tools,
		History:      c.history,
		SystemPrompt: c.systemPrompt,
		StreamOpts:   c.streamOpts,
		MaxTurns:     c.maxTurns,
		Hooks:        c.hooks,
		Extensions:   c.extensions,
	}
}

// Options bundles several options into one. A sub-package that creates
// options often has more than one to contribute, but it can return only
// a single [Option]:
//
//	func WithDirs(dirs ...string) agent.Option {
//	    opts := make([]agent.Option, 0, len(dirs))
//	    for _, d := range dirs {
//	        opts = append(opts, WithResolver(scan(d)))
//	    }
//	    return agent.Options(opts...)
//	}
//
// The bundled options apply in place and in order. They interleave with
// their neighbors exactly as if the caller passed each one inline.
func Options(opts ...Option) Option {
	return func(c *config) {
		for _, opt := range opts {
			opt(c)
		}
	}
}

// Factory builds an [Agent] from a "<kind>/<model>" spec and options. The
// catalog stores one factory for each custom agent kind, for example the
// subprocess CLIs. It routes to the factory by the kind prefix of the
// spec.
type Factory func(spec string, opts ...Option) (Agent, error)
