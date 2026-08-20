package claude

import (
	"fmt"
	"regexp"

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
	newSession      bool
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
	strictMCP       bool
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
	case ai.ThinkingMax:
		return string(ai.ThinkingMax)
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

// WithSessionID resumes the CLI conversation with this ID. The
// subprocess starts with --resume, and the CLI replays its own
// transcript for that session.
//
// The ID must be a UUID. The CLI accepts no other shape, and a caller
// whose session IDs look different needs a UUID of its own for this
// side. Use [WithNewSession] for a session the CLI has not seen yet:
// --resume on an unknown ID fails the run.
func WithSessionID(id string) agent.Option {
	return mutate(func(c *config) {
		c.sessionID = id
		c.newSession = false
	})
}

// WithNewSession creates the CLI conversation under this ID. The
// subprocess starts with --session-id, which names the session instead
// of letting the CLI generate one. A caller that already has a session
// identity — a durable session, a ticket, a thread — keeps one ID for
// both sides.
//
// Use it for the first run of a session and [WithSessionID] for every
// run after it. The CLI rejects --session-id for an ID it already
// holds, so the two are not interchangeable.
func WithNewSession(id string) agent.Option {
	return mutate(func(c *config) {
		c.sessionID = id
		c.newSession = true
	})
}

// sessionIDPattern is the UUID shape the Claude CLI requires. The
// package checks it here so a caller learns before a subprocess starts,
// rather than from its exit code.
var sessionIDPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

// validateSessionID makes sure that the CLI can use the session ID. An
// empty ID is valid: it means the CLI generates its own.
func validateSessionID(id string) error {
	if id == "" || sessionIDPattern.MatchString(id) {
		return nil
	}
	return fmt.Errorf(
		"claude: session ID %q is not a UUID; the CLI accepts no other shape",
		id,
	)
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

// WithStrictMCPConfig sets --strict-mcp-config. The CLI then uses only the
// servers of [WithMCPConfig], and ignores the ones its own settings files
// declare.
//
// Use it whenever the caller owns the server list. Without it the
// subprocess also connects whatever the person who operates the machine
// configured for themselves, which the caller did not choose and cannot
// see. The flag has no effect without [WithMCPConfig].
func WithStrictMCPConfig(strict bool) agent.Option {
	return mutate(func(c *config) { c.strictMCP = strict })
}

// WithPermissionMode sets --permission-mode for the session. Examples
// are "manual", "acceptEdits", "plan", and "bypassPermissions". The mode
// decides which tool calls the CLI's own policy asks about. To receive
// those questions as can_use_tool control requests, pair the mode with
// an [agent.HookBeforeTool] hook. An empty string omits the flag.
func WithPermissionMode(mode string) agent.Option {
	return mutate(func(c *config) { c.permissionMode = mode })
}
