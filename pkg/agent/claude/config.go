package claude

import (
	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

// extensionKey is the [agent.Config.Extensions] slot that the claude
// factory uses to carry its subprocess configuration.
const extensionKey = "claude"

// config holds all configuration for a Claude CLI subprocess agent.
type config struct {
	cliPath         string
	workDir         string
	addDirs         []string
	env             []string
	sessionID       string
	allowedTools    []string
	tools           []string
	toolsSet        bool
	disallowedTools []string
	maxTurns        int
	model           string
	thinkingLevel   ai.ThinkingLevel
	agent           string
	agents          map[string]AgentDef
	systemPrompt    string
	replacePrompt   bool
	mcpConfig       string
	permissionMode  string
	history         []ai.Message

	// beforeTool holds the [agent.HookBeforeTool] hooks registered with
	// [agent.WithHook]. If this field is not empty, the package starts
	// the subprocess with `--permission-prompt-tool stdio`. The CLI then
	// asks this process for tool approval with can_use_tool control
	// requests.
	beforeTool []agent.Hook
}

// mutate returns an [agent.Option] that applies fn to the claude-scoped
// config in [agent.Config.Extensions]. All claude options use this
// helper, so the factory sees one merged *config value.
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

// WithThinkingLevel sets the requested reasoning level. The Claude CLI
// states reasoning as a session effort. [effortForThinkingLevel] maps
// the level onto --effort. For "off" and for unknown levels, the package
// omits the flag and the CLI applies its own default.
func WithThinkingLevel(level ai.ThinkingLevel) agent.Option {
	return mutate(func(c *config) { c.thinkingLevel = level })
}

// effortForThinkingLevel maps a thinking level onto the --effort scale
// of the Claude CLI (low/medium/high/xhigh/max). The CLI has no "off"
// effort and no "minimal" effort. "off" and unknown levels return "",
// which omits the flag. "minimal" becomes "low". No thinking level maps
// to "max".
func effortForThinkingLevel(level ai.ThinkingLevel) string {
	switch level {
	case ai.ThinkingMinimal, ai.ThinkingLow:
		return string(ai.ThinkingLow)
	case ai.ThinkingMedium:
		return string(ai.ThinkingMedium)
	case ai.ThinkingHigh:
		return string(ai.ThinkingHigh)
	case ai.ThinkingXHigh:
		return string(ai.ThinkingXHigh)
	default:
		return ""
	}
}

// WithAddDirs adds more working directories with --add-dir flags.
func WithAddDirs(dirs ...string) agent.Option {
	return mutate(func(c *config) { c.addDirs = dirs })
}

// WithEnv sets more environment variables for the subprocess. Each entry
// must be in "KEY=VALUE" format.
func WithEnv(env ...string) agent.Option {
	return mutate(func(c *config) { c.env = env })
}

// WithSessionID sets an explicit session ID. The subprocess uses it with
// --resume to continue an earlier conversation.
func WithSessionID(id string) agent.Option {
	return mutate(func(c *config) { c.sessionID = id })
}

// WithAllowedTools sets the tools that the subprocess can use, with the
// --allowedTools flag.
func WithAllowedTools(tools ...string) agent.Option {
	return mutate(func(c *config) { c.allowedTools = tools })
}

// WithTools sets the available built-in tools with --tools. To disable
// all tools, pass no arguments (--tools ""). To limit the set, pass tool
// names such as "Bash", "Edit", or "Read".
func WithTools(tools ...string) agent.Option {
	return mutate(func(c *config) {
		c.tools = tools
		c.toolsSet = true
	})
}

// WithDisallowedTools denies tools with --disallowedTools. It accepts
// plain names ("Write") or patterns ("Bash(rm:*)").
func WithDisallowedTools(tools ...string) agent.Option {
	return mutate(func(c *config) { c.disallowedTools = tools })
}

// WithAgent selects a named agent with --agent.
func WithAgent(name string) agent.Option {
	return mutate(func(c *config) { c.agent = name })
}

// AgentDef defines one custom agent that --agents passes to the CLI.
type AgentDef struct {
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	Tools       []string `json:"tools,omitempty"`
	Model       string   `json:"model,omitempty"`
}

// WithAgents defines custom agents inline with --agents. The package
// marshals the map to JSON when it starts the subprocess.
func WithAgents(agents map[string]AgentDef) agent.Option {
	return mutate(func(c *config) { c.agents = agents })
}

// WithAppendPrompt controls how the system prompt reaches the CLI. The
// top-level [agent.WithSystemPrompt] sets that prompt. The value true,
// which is the default, passes the prompt as --append-system-prompt and
// adds it to Claude Code's own base prompt. The value false passes the
// prompt as --system-prompt and replaces the base prompt. There is one
// prompt and one switch. There is no second prompt channel.
func WithAppendPrompt(append bool) agent.Option {
	return mutate(func(c *config) { c.replacePrompt = !append })
}

// WithMCPConfig sets --mcp-config to spec. The Claude CLI accepts spec
// as a file path or as an inline JSON document. The document names the
// MCP servers to connect to. An empty string disables the flag.
func WithMCPConfig(spec string) agent.Option {
	return mutate(func(c *config) { c.mcpConfig = spec })
}

// WithPermissionMode sets --permission-mode for the session. Examples
// are "manual", "acceptEdits", "plan", and "bypassPermissions". The mode
// decides which tool calls the CLI's own policy asks about. To receive
// those questions as can_use_tool control requests, pair the mode with
// an [agent.HookBeforeTool] hook. An empty string omits the flag.
func WithPermissionMode(mode string) agent.Option {
	return mutate(func(c *config) { c.permissionMode = mode })
}
