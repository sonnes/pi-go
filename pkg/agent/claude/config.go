package claude

import (
	"github.com/sonnes/pi-go/pkg/agent"
)

// extensionKey is the [agent.Config.Extensions] slot used by the claude
// factory to carry its subprocess-specific configuration.
const extensionKey = "claude"

// config holds all configuration for a Claude CLI subprocess agent.
type config struct {
	cliPath            string
	workDir            string
	addDirs            []string
	env                []string
	sessionID          string
	allowedTools       []string
	tools              []string
	toolsSet           bool
	disallowedTools    []string
	maxTurns           int
	model              string
	agent              string
	agents             map[string]AgentDef
	systemPrompt       string
	appendSystemPrompt string
	history            []agent.Message
}

// mutate returns an [agent.Option] that applies fn to the claude-scoped
// config held in [agent.Config.Extensions]. All claude options share this
// helper so the factory sees a single, coalesced *config value.
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

// WithCLIPath sets the path to the claude CLI binary.
// Defaults to "claude".
func WithCLIPath(path string) agent.Option {
	return mutate(func(c *config) { c.cliPath = path })
}

// WithWorkDir sets the working directory for the subprocess.
func WithWorkDir(dir string) agent.Option {
	return mutate(func(c *config) { c.workDir = dir })
}

// WithAddDirs adds additional working directories via --add-dir flags.
func WithAddDirs(dirs ...string) agent.Option {
	return mutate(func(c *config) { c.addDirs = dirs })
}

// WithEnv sets additional environment variables for the subprocess.
// Each entry should be in "KEY=VALUE" format.
func WithEnv(env ...string) agent.Option {
	return mutate(func(c *config) { c.env = env })
}

// WithSessionID sets an explicit session ID. Used with --resume
// to continue a previous conversation.
func WithSessionID(id string) agent.Option {
	return mutate(func(c *config) { c.sessionID = id })
}

// WithAllowedTools sets the tools the subprocess is allowed to use
// via the --allowedTools flag.
func WithAllowedTools(tools ...string) agent.Option {
	return mutate(func(c *config) { c.allowedTools = tools })
}

// WithTools sets the available built-in tools via --tools. Pass no
// arguments to disable all tools (--tools ""), or tool names such as
// "Bash", "Edit", "Read" to restrict to that set.
func WithTools(tools ...string) agent.Option {
	return mutate(func(c *config) {
		c.tools = tools
		c.toolsSet = true
	})
}

// WithDisallowedTools denies tools via --disallowedTools. Accepts plain
// names ("Write") or patterns ("Bash(rm:*)").
func WithDisallowedTools(tools ...string) agent.Option {
	return mutate(func(c *config) { c.disallowedTools = tools })
}

// WithAgent selects a named agent via --agent.
func WithAgent(name string) agent.Option {
	return mutate(func(c *config) { c.agent = name })
}

// AgentDef defines a single custom agent passed via --agents.
type AgentDef struct {
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	Tools       []string `json:"tools,omitempty"`
	Model       string   `json:"model,omitempty"`
}

// WithAgents defines custom agents inline via --agents.
// The map is marshaled to JSON when the subprocess is spawned.
func WithAgents(agents map[string]AgentDef) agent.Option {
	return mutate(func(c *config) { c.agents = agents })
}

// WithSystemPrompt replaces the default system prompt via --system-prompt.
func WithSystemPrompt(prompt string) agent.Option {
	return mutate(func(c *config) { c.systemPrompt = prompt })
}

// WithAppendSystemPrompt appends to the default system prompt via --append-system-prompt.
func WithAppendSystemPrompt(prompt string) agent.Option {
	return mutate(func(c *config) { c.appendSystemPrompt = prompt })
}
