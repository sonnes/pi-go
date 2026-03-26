package claude

import (
	"github.com/sonnes/pi-go/pkg/agent"
)

// config holds all configuration for a Claude CLI subprocess agent.
type config struct {
	cliPath      string
	workDir      string
	addDirs      []string
	env          []string
	sessionID    string
	allowedTools []string
	maxTurns     int
	model        string
	history      []agent.Message
}

// Option configures a Claude CLI subprocess [Agent].
type Option func(*config)

// WithCLIPath sets the path to the claude CLI binary.
// Defaults to "claude".
func WithCLIPath(path string) Option {
	return func(c *config) { c.cliPath = path }
}

// WithWorkDir sets the working directory for the subprocess.
func WithWorkDir(dir string) Option {
	return func(c *config) { c.workDir = dir }
}

// WithAddDirs adds additional working directories via --add-dir flags.
func WithAddDirs(dirs ...string) Option {
	return func(c *config) { c.addDirs = dirs }
}

// WithEnv sets additional environment variables for the subprocess.
// Each entry should be in "KEY=VALUE" format.
func WithEnv(env ...string) Option {
	return func(c *config) { c.env = env }
}

// WithSessionID sets an explicit session ID. Used with --resume
// to continue a previous conversation.
func WithSessionID(id string) Option {
	return func(c *config) { c.sessionID = id }
}

// WithAllowedTools sets the tools the subprocess is allowed to use
// via the --allowedTools flag.
func WithAllowedTools(tools ...string) Option {
	return func(c *config) { c.allowedTools = tools }
}

// WithMaxTurns limits the number of agentic turns via --max-turns.
func WithMaxTurns(n int) Option {
	return func(c *config) { c.maxTurns = n }
}

// WithModel overrides the model via --model.
func WithModel(model string) Option {
	return func(c *config) { c.model = model }
}

// WithHistory sets the initial conversation messages.
func WithHistory(msgs ...agent.Message) Option {
	return func(c *config) { c.history = msgs }
}
