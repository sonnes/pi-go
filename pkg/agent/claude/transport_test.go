package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildArgs(t *testing.T) {
	base := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
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
			name: "with agent",
			cfg:  config{cliPath: "claude", agent: "reviewer"},
			want: append(append([]string{}, base...), "--agent", "reviewer"),
		},
		{
			name: "with system prompt",
			cfg:  config{cliPath: "claude", systemPrompt: "be terse"},
			want: append(append([]string{}, base...), "--system-prompt", "be terse"),
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
			name: "with append system prompt",
			cfg:  config{cliPath: "claude", appendSystemPrompt: "also be kind"},
			want: append(append([]string{}, base...), "--append-system-prompt", "also be kind"),
		},
		{
			name: "all options",
			cfg: config{
				cliPath:            "claude",
				model:              "sonnet",
				allowedTools:       []string{"Read"},
				tools:              []string{"Bash", "Edit"},
				disallowedTools:    []string{"Write"},
				maxTurns:           3,
				addDirs:            []string{"/extra"},
				agent:              "reviewer",
				agents:             map[string]AgentDef{"x": {Description: "xd", Prompt: "xp"}},
				systemPrompt:       "sys",
				appendSystemPrompt: "more",
				sessionID:          "sess-abc",
				mcpConfig:          `{"mcpServers":{}}`,
			},
			want: append(append([]string{}, base...),
				"--model", "sonnet",
				"--allowedTools", "Read",
				"--tools", "Bash,Edit",
				"--disallowedTools", "Write",
				"--mcp-config", `{"mcpServers":{}}`,
				"--max-turns", "3",
				"--add-dir", "/extra",
				"--agent", "reviewer",
				"--agents", `{"x":{"description":"xd","prompt":"xp"}}`,
				"--system-prompt", "sys",
				"--append-system-prompt", "more",
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
