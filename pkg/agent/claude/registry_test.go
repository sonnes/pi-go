package claude

import (
	"testing"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeNew_BuildsAgent(t *testing.T) {
	a := New(ai.Model{ID: "sonnet", Name: "sonnet"})
	require.NotNil(t, a)
	assert.Equal(t, "sonnet", a.cfg.model)
}

func TestClaudeNew_UsesModelID(t *testing.T) {
	// The CLI agent uses Model.Name, falling back to Model.ID when Name
	// is empty, as the model name it forwards to the subprocess.
	a := New(ai.Model{ID: "claude-sonnet-4-5"})
	assert.Equal(t, "claude-sonnet-4-5", a.cfg.model)
}

func TestClaudeNew_ComposesAgentAndClaudeOptions(t *testing.T) {
	// Agent-level and claude-specific options flow through a single slice.
	a := New(
		ai.Model{ID: "sonnet", Name: "sonnet"},
		agent.WithMaxTurns(7),
		WithCLIPath("/usr/local/bin/claude"),
		WithAllowedTools("Read", "Edit"),
		WithSessionID("sess-xyz"),
		WithThinkingLevel(ai.ThinkingHigh),
		WithMCPConfig(`{"mcpServers":{"lw":{"type":"http","url":"http://127.0.0.1:0/mcp"}}}`),
	)

	assert.Equal(t, "sonnet", a.cfg.model)
	assert.Equal(t, ai.ThinkingHigh, a.cfg.thinkingLevel)
	assert.Equal(t, 7, a.cfg.maxTurns)
	assert.Equal(t, "/usr/local/bin/claude", a.cfg.cliPath)
	assert.Equal(t, []string{"Read", "Edit"}, a.cfg.allowedTools)
	assert.Equal(t, "sess-xyz", a.cfg.sessionID)
	assert.Equal(t, `{"mcpServers":{"lw":{"type":"http","url":"http://127.0.0.1:0/mcp"}}}`, a.cfg.mcpConfig)
}

func TestClaudeNew_ConsumesTopLevelSystemPrompt(t *testing.T) {
	a := New(ai.Model{}, agent.WithSystemPrompt("You are a helper.\n\nBe concise."))
	assert.Equal(t, "You are a helper.\n\nBe concise.", a.cfg.systemPrompt)
}
