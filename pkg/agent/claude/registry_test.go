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
	agent.RegisterAgent("claude", New)
	t.Cleanup(func() { agent.UnregisterAgent("claude") })
}

func TestClaudeFactory_Registered(t *testing.T) {
	registerForTest(t)

	f, ok := agent.GetAgent("claude")
	require.True(t, ok)

	a := f(ai.Model{ID: "sonnet", Name: "sonnet"})
	require.NotNil(t, a)

	ca, ok := a.(*Agent)
	require.True(t, ok)
	assert.Equal(t, "sonnet", ca.cfg.model)
}

func TestClaudeFactory_UsesModelID(t *testing.T) {
	registerForTest(t)

	// The CLI agent uses Model.Name, falling back to Model.ID when Name
	// is empty, as the model name it forwards to the subprocess.
	f, _ := agent.GetAgent("claude")
	a := f(ai.Model{ID: "claude-sonnet-4-5"})
	ca := a.(*Agent)
	assert.Equal(t, "claude-sonnet-4-5", ca.cfg.model)
}

func TestClaudeFactory_ComposesAgentAndClaudeOptions(t *testing.T) {
	registerForTest(t)

	// Agent-level and claude-specific options flow through a single slice.
	f, _ := agent.GetAgent("claude")
	a := f(
		ai.Model{ID: "sonnet", Name: "sonnet"},
		agent.WithMaxTurns(7),
		WithCLIPath("/usr/local/bin/claude"),
		WithAllowedTools("Read", "Edit"),
		WithSessionID("sess-xyz"),
		WithThinkingLevel(ai.ThinkingHigh),
		WithMCPConfig(`{"mcpServers":{"lw":{"type":"http","url":"http://127.0.0.1:0/mcp"}}}`),
	)
	ca := a.(*Agent)

	assert.Equal(t, "sonnet", ca.cfg.model)
	assert.Equal(t, ai.ThinkingHigh, ca.cfg.thinkingLevel)
	assert.Equal(t, 7, ca.cfg.maxTurns)
	assert.Equal(t, "/usr/local/bin/claude", ca.cfg.cliPath)
	assert.Equal(t, []string{"Read", "Edit"}, ca.cfg.allowedTools)
	assert.Equal(t, "sess-xyz", ca.cfg.sessionID)
	assert.Equal(t, `{"mcpServers":{"lw":{"type":"http","url":"http://127.0.0.1:0/mcp"}}}`, ca.cfg.mcpConfig)
}

func TestClaudeFactory_ConsumesTopLevelSystemPrompt(t *testing.T) {
	registerForTest(t)

	f, _ := agent.GetAgent("claude")
	a := f(ai.Model{}, agent.WithSystemPrompt("You are a helper.\n\nBe concise."))
	ca := a.(*Agent)

	assert.Equal(t, "You are a helper.\n\nBe concise.", ca.cfg.systemPrompt)
}
