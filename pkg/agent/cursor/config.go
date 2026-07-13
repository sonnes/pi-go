package cursor

import (
	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

const extensionKey = "cursor"

type config struct {
	cliPath      string
	workDir      string
	env          []string
	apiKey       string
	headers      []string
	sessionID    string
	model        string
	mode         string
	sandbox      string
	force        bool
	approveMCPs  bool
	browser      bool
	maxTurns     int
	systemPrompt string
	history      []ai.Message
}

func mutate(fn func(*config)) agent.Option {
	return agent.WithExtensionMutator(extensionKey, func(v any) any {
		cfg, _ := v.(*config)
		if cfg == nil {
			cfg = &config{}
		}
		fn(cfg)
		return cfg
	})
}

// WithCLIPath sets the path to the cursor-agent CLI binary. Defaults to
// "cursor-agent".
func WithCLIPath(path string) agent.Option {
	return mutate(func(c *config) { c.cliPath = path })
}

// WithWorkDir sets the workspace directory for the subprocess.
func WithWorkDir(dir string) agent.Option {
	return mutate(func(c *config) { c.workDir = dir })
}

// WithEnv sets additional environment variables for the subprocess.
// Each entry should be in "KEY=VALUE" format.
func WithEnv(env ...string) agent.Option {
	return mutate(func(c *config) { c.env = env })
}

// WithAPIKey passes a Cursor API key via --api-key. Most callers should prefer
// CURSOR_API_KEY or `cursor-agent login`.
func WithAPIKey(key string) agent.Option {
	return mutate(func(c *config) { c.apiKey = key })
}

// WithHeaders adds custom request headers in "Name: Value" form.
func WithHeaders(headers ...string) agent.Option {
	return mutate(func(c *config) { c.headers = headers })
}

// WithSessionID seeds the agent with a Cursor chat session ID. The next Send
// resumes that chat with `cursor-agent --resume`.
func WithSessionID(id string) agent.Option {
	return mutate(func(c *config) { c.sessionID = id })
}

// WithMode sets Cursor's read-only execution mode, such as "ask" or "plan".
func WithMode(mode string) agent.Option {
	return mutate(func(c *config) { c.mode = mode })
}

// WithSandbox sets Cursor's sandbox mode, such as "enabled" or "disabled".
func WithSandbox(mode string) agent.Option {
	return mutate(func(c *config) { c.sandbox = mode })
}

// WithForce passes --force so Cursor can run allowed commands without
// interactive approval.
func WithForce() agent.Option {
	return mutate(func(c *config) { c.force = true })
}

// WithApproveMCPs passes --approve-mcps for headless MCP approval.
func WithApproveMCPs() agent.Option {
	return mutate(func(c *config) { c.approveMCPs = true })
}

// WithBrowser enables Cursor CLI browser automation support.
func WithBrowser() agent.Option {
	return mutate(func(c *config) { c.browser = true })
}
