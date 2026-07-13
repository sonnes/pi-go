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

// WithAddDirs adds additional writable directories via --add-dir flags.
func WithAddDirs(dirs ...string) agent.Option {
	return mutate(func(c *config) { c.addDirs = dirs })
}

// WithEnv sets additional environment variables for the subprocess.
// Each entry should be in "KEY=VALUE" format.
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
// reasoning as `model_reasoning_effort`, so the level is mapped onto a
// `-c model_reasoning_effort=…` override by [reasoningEffortForThinkingLevel].
func WithThinkingLevel(level ai.ThinkingLevel) agent.Option {
	return mutate(func(c *config) { c.thinkingLevel = level })
}

// reasoningEffortForThinkingLevel maps a thinking level onto Codex's
// model_reasoning_effort scale (minimal/low/medium/high/xhigh). Codex
// has no "off": "off"/unknown return "" (omit the override); every other
// level maps through unchanged. "xhigh" is model-dependent, so Codex
// applies its own fallback when the active model does not support it.
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

// WithApprovalPolicy sets the Codex approval policy. Defaults to "never"
// so non-interactive sends cannot hang waiting for terminal approval.
func WithApprovalPolicy(policy string) agent.Option {
	return mutate(func(c *config) { c.approvalPolicy = policy })
}

// WithSkipGitRepoCheck allows running Codex outside a Git repository.
func WithSkipGitRepoCheck() agent.Option {
	return mutate(func(c *config) { c.skipGitRepoCheck = true })
}

// WithIgnoreUserConfig prevents loading $CODEX_HOME/config.toml.
func WithIgnoreUserConfig() agent.Option {
	return mutate(func(c *config) { c.ignoreUserConfig = true })
}

// WithIgnoreRules prevents loading user or project execpolicy rules.
func WithIgnoreRules() agent.Option {
	return mutate(func(c *config) { c.ignoreRules = true })
}
