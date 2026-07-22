package claude

import (
	"context"
	"testing"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pin the exact stream-json interrupt control_request wire format the Claude
// CLI accepts on stdin (mirrors the Agent SDK). A drift here breaks Abort.
func TestBuildInterruptControl(t *testing.T) {
	line, err := buildInterruptControl("req_1")
	require.NoError(t, err)
	assert.Equal(t,
		`{"type":"control_request","request_id":"req_1","request":{"subtype":"interrupt"}}`+"\n",
		string(line),
	)
}

// Pin the control_response wire format for can_use_tool permission
// replies. The CLI blocks tool execution until this line arrives on
// stdin; a drift here deadlocks the subprocess.
func TestBuildPermissionResponse(t *testing.T) {
	t.Run("allow echoes input", func(t *testing.T) {
		line, err := buildPermissionResponse(
			"req-1",
			true,
			map[string]any{"command": "ls"},
			"",
		)
		require.NoError(t, err)
		assert.Equal(t,
			`{"type":"control_response","response":{"subtype":"success","request_id":"req-1","response":{"behavior":"allow","updatedInput":{"command":"ls"}}}}`+"\n",
			string(line),
		)
	})

	t.Run("deny carries message", func(t *testing.T) {
		line, err := buildPermissionResponse(
			"req-2",
			false,
			nil,
			"denied by user",
		)
		require.NoError(t, err)
		assert.Equal(t,
			`{"type":"control_response","response":{"subtype":"success","request_id":"req-2","response":{"behavior":"deny","message":"denied by user"}}}`+"\n",
			string(line),
		)
	})
}

func TestBuildArgs(t *testing.T) {
	base := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
	}

	tests := []struct {
		name string
		cfg  config
		want []string
	}{
		{
			name: "defaults only",
			cfg:  config{cliPath: "claude"},
			want: base,
		},
		{
			name: "with model",
			cfg:  config{cliPath: "claude", model: "opus"},
			want: append(append([]string{}, base...), "--model", "opus"),
		},
		{
			name: "thinking level maps to effort",
			cfg:  config{cliPath: "claude", thinkingLevel: ai.ThinkingHigh},
			want: append(append([]string{}, base...), "--effort", "high"),
		},
		{
			name: "minimal thinking floors to low effort",
			cfg:  config{cliPath: "claude", thinkingLevel: ai.ThinkingMinimal},
			want: append(append([]string{}, base...), "--effort", "low"),
		},
		{
			name: "off thinking omits effort",
			cfg:  config{cliPath: "claude", thinkingLevel: ai.ThinkingOff},
			want: base,
		},
		{
			name: "with allowed tools",
			cfg:  config{cliPath: "claude", allowedTools: []string{"Read", "Edit", "Bash"}},
			want: append(append([]string{}, base...), "--allowedTools", "Read,Edit,Bash"),
		},
		{
			name: "with tools",
			cfg:  config{cliPath: "claude", tools: []string{"Bash", "Read"}},
			want: append(append([]string{}, base...), "--tools", "Bash,Read"),
		},
		{
			name: "with tools empty disables all",
			cfg:  config{cliPath: "claude", tools: []string{}, toolsSet: true},
			want: append(append([]string{}, base...), "--tools", ""),
		},
		{
			name: "with disallowed tools",
			cfg:  config{cliPath: "claude", disallowedTools: []string{"Bash(rm:*)", "Write"}},
			want: append(append([]string{}, base...), "--disallowedTools", "Bash(rm:*),Write"),
		},
		{
			name: "with mcp config",
			cfg:  config{cliPath: "claude", mcpConfig: `{"mcpServers":{"lw":{"type":"http","url":"http://127.0.0.1:1/mcp"}}}`},
			want: append(append([]string{}, base...), "--mcp-config", `{"mcpServers":{"lw":{"type":"http","url":"http://127.0.0.1:1/mcp"}}}`),
		},
		{
			name: "with max turns",
			cfg:  config{cliPath: "claude", maxTurns: 5},
			want: append(append([]string{}, base...), "--max-turns", "5"),
		},
		{
			name: "with add dirs",
			cfg:  config{cliPath: "claude", addDirs: []string{"/a", "/b"}},
			want: append(append([]string{}, base...), "--add-dir", "/a", "--add-dir", "/b"),
		},
		{
			name: "with session ID resumes",
			cfg:  config{cliPath: "claude", sessionID: "sess-123"},
			want: append(append([]string{}, base...), "--resume", "sess-123"),
		},
		{
			name: "before-tool hooks enable the stdio permission prompt",
			cfg: config{
				cliPath: "claude",
				beforeTool: []agent.Hook{
					func(context.Context, *agent.HookInput) (*agent.HookOutput, error) {
						return nil, nil
					},
				},
			},
			want: append(append([]string{}, base...), "--permission-prompt-tool", "stdio"),
		},
		{
			name: "with permission mode",
			cfg:  config{cliPath: "claude", permissionMode: "manual"},
			want: append(append([]string{}, base...), "--permission-mode", "manual"),
		},
		{
			name: "with agent",
			cfg:  config{cliPath: "claude", agent: "reviewer"},
			want: append(append([]string{}, base...), "--agent", "reviewer"),
		},
		{
			name: "with system prompt appends by default",
			cfg:  config{cliPath: "claude", systemPrompt: "be terse"},
			want: append(append([]string{}, base...), "--append-system-prompt", "be terse"),
		},
		{
			name: "with agents",
			cfg: config{
				cliPath: "claude",
				agents:  map[string]AgentDef{"reviewer": {Description: "d", Prompt: "p"}},
			},
			want: append(append([]string{}, base...),
				"--agents", `{"reviewer":{"description":"d","prompt":"p"}}`,
			),
		},
		{
			name: "replace prompt switches the flag",
			cfg:  config{cliPath: "claude", systemPrompt: "also be kind", replacePrompt: true},
			want: append(append([]string{}, base...), "--system-prompt", "also be kind"),
		},
		{
			name: "all options",
			cfg: config{
				cliPath:         "claude",
				model:           "sonnet",
				thinkingLevel:   ai.ThinkingXHigh,
				allowedTools:    []string{"Read"},
				tools:           []string{"Bash", "Edit"},
				disallowedTools: []string{"Write"},
				maxTurns:        3,
				addDirs:         []string{"/extra"},
				agent:           "reviewer",
				agents:          map[string]AgentDef{"x": {Description: "xd", Prompt: "xp"}},
				systemPrompt:    "sys",
				sessionID:       "sess-abc",
				mcpConfig:       `{"mcpServers":{}}`,
			},
			want: append(append([]string{}, base...),
				"--model", "sonnet",
				"--effort", "xhigh",
				"--allowedTools", "Read",
				"--tools", "Bash,Edit",
				"--disallowedTools", "Write",
				"--mcp-config", `{"mcpServers":{}}`,
				"--max-turns", "3",
				"--add-dir", "/extra",
				"--agent", "reviewer",
				"--agents", `{"x":{"description":"xd","prompt":"xp"}}`,
				"--append-system-prompt", "sys",
				"--resume", "sess-abc",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildArgs(tt.cfg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEffortForThinkingLevel(t *testing.T) {
	tests := []struct {
		level ai.ThinkingLevel
		want  string
	}{
		{level: "", want: ""},
		{level: ai.ThinkingOff, want: ""},
		{level: ai.ThinkingMinimal, want: "low"},
		{level: ai.ThinkingLow, want: "low"},
		{level: ai.ThinkingMedium, want: "medium"},
		{level: ai.ThinkingHigh, want: "high"},
		{level: ai.ThinkingXHigh, want: "xhigh"},
		{level: "bogus", want: ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			assert.Equal(t, tt.want, effortForThinkingLevel(tt.level))
		})
	}
}
