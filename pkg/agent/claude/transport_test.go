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
			name: "all options",
			cfg: config{
				cliPath:      "claude",
				model:        "sonnet",
				allowedTools: []string{"Read"},
				maxTurns:     3,
				addDirs:      []string{"/extra"},
			},
			args: sendArgs{prompt: "go"},
			want: []string{
				"--print",
				"--output-format", "stream-json",
				"--verbose",
				"--model", "sonnet",
				"--allowedTools", "Read",
				"--max-turns", "3",
				"--add-dir", "/extra",
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
