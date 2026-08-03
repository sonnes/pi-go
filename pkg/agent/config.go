package agent

import "github.com/sonnes/pi-go/pkg/ai"

// Config is a snapshot of the configuration applied by a set of [Option]
// values — the exported view of the internal config, for sub-packages
// that read options without importing unexported types.
//
// Extensions holds values set by [WithExtension]/[WithExtensionMutator],
// keyed by the owning sub-package (by convention its package name).
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
// The model is passed separately to [New], not via options.
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

// Options bundles several options into one. A sub-package minting
// options into the shared currency often has more than one to
// contribute but only a single [Option] to return:
//
//	func WithDirs(dirs ...string) agent.Option {
//	    opts := make([]agent.Option, 0, len(dirs))
//	    for _, d := range dirs {
//	        opts = append(opts, WithResolver(scan(d)))
//	    }
//	    return agent.Options(opts...)
//	}
//
// The bundled options apply in place, in order, so they interleave with
// their neighbours exactly as if they had been passed inline.
func Options(opts ...Option) Option {
	return func(c *config) {
		for _, opt := range opts {
			opt(c)
		}
	}
}

// Factory builds an [Agent] from a "<kind>/<model>" spec and options. The
// catalog stores one per custom agent kind (e.g. the subprocess CLIs) and
// routes to it by the spec's kind prefix.
type Factory func(spec string, opts ...Option) (Agent, error)
