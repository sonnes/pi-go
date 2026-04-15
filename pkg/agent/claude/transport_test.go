package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		args sendArgs
		want []string
	}{
		{
			name: "simple prompt",
			cfg:  config{cliPath: "claude"},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"hello",
			},
		},
		{
			name: "with model",
			cfg:  config{cliPath: "claude", model: "opus"},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--model", "opus",
				"hello",
			},
		},
		{
			name: "with allowed tools",
			cfg:  config{cliPath: "claude", allowedTools: []string{"Read", "Edit", "Bash"}},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--allowedTools", "Read,Edit,Bash",
				"hello",
			},
		},
		{
			name: "with tools",
			cfg:  config{cliPath: "claude", tools: []string{"Bash", "Read"}},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--tools", "Bash,Read",
				"hello",
			},
		},
		{
			name: "with tools empty disables all",
			cfg:  config{cliPath: "claude", tools: []string{}, toolsSet: true},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--tools", "",
				"hello",
			},
		},
		{
			name: "with disallowed tools",
			cfg:  config{cliPath: "claude", disallowedTools: []string{"Bash(rm:*)", "Write"}},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--disallowedTools", "Bash(rm:*),Write",
				"hello",
			},
		},
		{
			name: "with max turns",
			cfg:  config{cliPath: "claude", maxTurns: 5},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--max-turns", "5",
				"hello",
			},
		},
		{
			name: "with add dirs",
			cfg:  config{cliPath: "claude", addDirs: []string{"/a", "/b"}},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--add-dir", "/a",
				"--add-dir", "/b",
				"hello",
			},
		},
		{
			name: "resume with session ID",
			cfg:  config{cliPath: "claude"},
			args: sendArgs{resume: true, sessionID: "sess-123"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--resume", "sess-123",
			},
		},
		{
			name: "resume with prompt continues",
			cfg:  config{cliPath: "claude"},
			args: sendArgs{prompt: "continue this", resume: true, sessionID: "sess-123"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--resume", "sess-123",
				"continue this",
			},
		},
		{
			name: "with agent",
			cfg:  config{cliPath: "claude", agent: "reviewer"},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--agent", "reviewer",
				"hello",
			},
		},
		{
			name: "with system prompt",
			cfg:  config{cliPath: "claude", systemPrompt: "be terse"},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--system-prompt", "be terse",
				"hello",
			},
		},
		{
			name: "with agents",
			cfg: config{
				cliPath: "claude",
				agents: map[string]AgentDef{
					"reviewer": {Description: "d", Prompt: "p"},
				},
			},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--agents", `{"reviewer":{"description":"d","prompt":"p"}}`,
				"hello",
			},
		},
		{
			name: "with append system prompt",
			cfg:  config{cliPath: "claude", appendSystemPrompt: "also be kind"},
			args: sendArgs{prompt: "hello"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--append-system-prompt", "also be kind",
				"hello",
			},
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
			},
			args: sendArgs{prompt: "go"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--model", "sonnet",
				"--allowedTools", "Read",
				"--tools", "Bash,Edit",
				"--disallowedTools", "Write",
				"--max-turns", "3",
				"--add-dir", "/extra",
				"--agent", "reviewer",
				"--agents", `{"x":{"description":"xd","prompt":"xp"}}`,
				"--system-prompt", "sys",
				"--append-system-prompt", "more",
				"go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildArgs(tt.cfg, tt.args)
			assert.Equal(t, tt.want, got)
		})
	}
}
