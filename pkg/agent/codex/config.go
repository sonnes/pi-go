package codex

import (
	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

const extensionKey = "codex"

type config struct {
	cliPath          string
	workDir          string
	addDirs          []string
	env              []string
	sessionID        string
	model            string
	thinkingLevel    ai.ThinkingLevel
	sandbox          string
	approvalPolicy   string
	skipGitRepoCheck bool
	ignoreUserConfig bool
	ignoreRules      bool
	maxTurns         int
	systemPrompt     string
	history          []ai.Message
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

// WithCLIPath sets the path to the codex CLI binary. Defaults to "codex".
func WithCLIPath(path string) agent.Option {
	return mutate(func(c *config) { c.cliPath = path })
}

// WithWorkDir sets the working root for the subprocess.
func WithWorkDir(dir string) agent.Option {
	return mutate(func(c *config) { c.workDir = dir })
}

// WithAddDirs adds more writable directories through --add-dir flags.
func WithAddDirs(dirs ...string) agent.Option {
	return mutate(func(c *config) { c.addDirs = dirs })
}

// WithEnv sets more environment variables for the subprocess. Each entry
// must have the "KEY=VALUE" format.
func WithEnv(env ...string) agent.Option {
	return mutate(func(c *config) { c.env = env })
}

// WithSessionID seeds the agent with a Codex thread ID. The next Send
// resumes that thread with `codex exec resume`.
func WithSessionID(id string) agent.Option {
	return mutate(func(c *config) { c.sessionID = id })
}

// WithSandbox sets the Codex sandbox mode, such as "read-only",
// "workspace-write", or "danger-full-access".
func WithSandbox(mode string) agent.Option {
	return mutate(func(c *config) { c.sandbox = mode })
}

// WithThinkingLevel sets the requested reasoning level. Codex expresses
// reasoning as `model_reasoning_effort`. [reasoningEffortForThinkingLevel]
// maps the level onto a `-c model_reasoning_effort=…` override.
func WithThinkingLevel(level ai.ThinkingLevel) agent.Option {
	return mutate(func(c *config) { c.thinkingLevel = level })
}

// reasoningEffortForThinkingLevel maps a thinking level onto the
// model_reasoning_effort scale of Codex (minimal/low/medium/high/xhigh).
// Codex has no "off" level. An "off" or unknown level returns "", and the
// caller omits the override. Every other level maps through unchanged.
// The "xhigh" level depends on the model, so Codex applies its own
// fallback when the active model does not support it.
func reasoningEffortForThinkingLevel(level ai.ThinkingLevel) string {
	switch level {
	case ai.ThinkingMinimal,
		ai.ThinkingLow,
		ai.ThinkingMedium,
		ai.ThinkingHigh,
		ai.ThinkingXHigh:
		return string(level)
	default:
		return ""
	}
}

// WithApprovalPolicy sets the Codex approval policy. The default is
// "never", so a non-interactive send does not wait for terminal approval.
func WithApprovalPolicy(policy string) agent.Option {
	return mutate(func(c *config) { c.approvalPolicy = policy })
}

// WithSkipGitRepoCheck lets Codex run outside a Git repository.
func WithSkipGitRepoCheck() agent.Option {
	return mutate(func(c *config) { c.skipGitRepoCheck = true })
}

// WithIgnoreUserConfig stops Codex from loading $CODEX_HOME/config.toml.
func WithIgnoreUserConfig() agent.Option {
	return mutate(func(c *config) { c.ignoreUserConfig = true })
}

// WithIgnoreRules stops Codex from loading user or project execpolicy
// rules.
func WithIgnoreRules() agent.Option {
	return mutate(func(c *config) { c.ignoreRules = true })
}
