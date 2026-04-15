package claude

import (
	"github.com/sonnes/pi-go/pkg/agent"
)

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

// WithTools sets the available built-in tools via --tools. Pass no
// arguments to disable all tools (--tools ""), or tool names such as
// "Bash", "Edit", "Read" to restrict to that set.
func WithTools(tools ...string) Option {
	return func(c *config) {
		c.tools = tools
		c.toolsSet = true
	}
}

// WithDisallowedTools denies tools via --disallowedTools. Accepts plain
// names ("Write") or patterns ("Bash(rm:*)").
func WithDisallowedTools(tools ...string) Option {
	return func(c *config) { c.disallowedTools = tools }
}

// WithMaxTurns limits the number of agentic turns via --max-turns.
func WithMaxTurns(n int) Option {
	return func(c *config) { c.maxTurns = n }
}

// WithModel overrides the model via --model.
func WithModel(model string) Option {
	return func(c *config) { c.model = model }
}

// WithAgent selects a named agent via --agent.
func WithAgent(name string) Option {
	return func(c *config) { c.agent = name }
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
func WithAgents(agents map[string]AgentDef) Option {
	return func(c *config) { c.agents = agents }
}

// WithSystemPrompt replaces the default system prompt via --system-prompt.
func WithSystemPrompt(prompt string) Option {
	return func(c *config) { c.systemPrompt = prompt }
}

// WithAppendSystemPrompt appends to the default system prompt via --append-system-prompt.
func WithAppendSystemPrompt(prompt string) Option {
	return func(c *config) { c.appendSystemPrompt = prompt }
}

// WithHistory sets the initial conversation messages.
func WithHistory(msgs ...agent.Message) Option {
	return func(c *config) { c.history = msgs }
}
