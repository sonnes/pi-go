package claude

import (
	"testing"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerForTest installs the claude factory under "claude" and removes it
// when the test completes. Registration is explicit (not via init()) so the
// pkg/agent package stays decoupled from concrete implementations.
func registerForTest(t *testing.T) {
	t.Helper()
	agent.RegisterFactory("claude", Factory)
	t.Cleanup(func() { agent.UnregisterFactory("claude") })
}

func TestClaudeFactory_Registered(t *testing.T) {
	registerForTest(t)

	f, ok := agent.GetFactory("claude")
	require.True(t, ok)

	a := f(agent.WithModelName("sonnet"))
	require.NotNil(t, a)

	ca, ok := a.(*Agent)
	require.True(t, ok)
	assert.Equal(t, "sonnet", ca.cfg.model)
}

func TestClaudeFactory_IgnoresModelID(t *testing.T) {
	registerForTest(t)

	// The factory only consumes Model.Name (set by agent.WithModelName);
	// a Model with only ID populated is ignored so the CLI picks its default.
	f, _ := agent.GetFactory("claude")
	a := f(agent.WithModel(ai.Model{ID: "claude-sonnet-4-5"}))
	ca := a.(*Agent)
	assert.Empty(t, ca.cfg.model)
}

func TestClaudeFactory_ComposesAgentAndClaudeOptions(t *testing.T) {
	registerForTest(t)

	// Agent-level and claude-specific options flow through a single slice.
	f, _ := agent.GetFactory("claude")
	a := f(
		agent.WithModelName("sonnet"),
		agent.WithMaxTurns(7),
		WithCLIPath("/usr/local/bin/claude"),
		WithAllowedTools("Read", "Edit"),
		WithSessionID("sess-xyz"),
		WithMCPConfig(`{"mcpServers":{"lw":{"type":"http","url":"http://127.0.0.1:0/mcp"}}}`),
	)
	ca := a.(*Agent)

	assert.Equal(t, "sonnet", ca.cfg.model)
	assert.Equal(t, 7, ca.cfg.maxTurns)
	assert.Equal(t, "/usr/local/bin/claude", ca.cfg.cliPath)
	assert.Equal(t, []string{"Read", "Edit"}, ca.cfg.allowedTools)
	assert.Equal(t, "sess-xyz", ca.cfg.sessionID)
	assert.Equal(t, `{"mcpServers":{"lw":{"type":"http","url":"http://127.0.0.1:0/mcp"}}}`, ca.cfg.mcpConfig)
}

func TestClaudeFactory_ConsumesTopLevelSystemPrompt(t *testing.T) {
	registerForTest(t)

	f, _ := agent.GetFactory("claude")
	a := f(agent.WithSystemPrompt("You are a helper.\n\nBe concise."))
	ca := a.(*Agent)

	assert.Equal(t, "You are a helper.\n\nBe concise.", ca.cfg.systemPrompt)
}
