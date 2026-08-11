package prompt_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
	"github.com/sonnes/pi-go/pkg/session"
)

func TestDefault(t *testing.T) {
	tests := []struct {
		name string
		env  *prompt.Env
		want string
	}{
		{
			name: "bare",
			env:  &prompt.Env{},
			want: `You are an agent. Work the task through to completion with the tools you have, and stop when it is done rather than asking what to do next.`,
		},
		{
			name: "tools",
			env: &prompt.Env{
				Tools: []ai.ToolInfo{
					{Name: "read", Description: "Read a file from disk.\nSupports ranges.", UseWhen: "You need a file's contents."},
					{Name: "write", Description: "Write a file to disk."},
				},
			},
			want: `You are an agent. Work the task through to completion with the tools you have, and stop when it is done rather than asking what to do next.

## Tools

- read: You need a file's contents.
- write: Write a file to disk.`,
		},
		{
			name: "skills",
			env: &prompt.Env{
				Skills: []def.Skill{
					{Name: "commit", Description: "Writes a commit message."},
				},
			},
			want: `You are an agent. Work the task through to completion with the tools you have, and stop when it is done rather than asking what to do next.

## Skills

Skills are instructions you load on demand with the ` + "`skill`" + ` tool. Load the one that covers the task before starting it.

- commit: Writes a commit message.`,
		},
		{
			name: "every instruction document is rendered, in resolver order",
			env: &prompt.Env{
				Instructions: []def.Instructions{
					{Source: "AGENTS.md", Content: "Run make test."},
					{Source: "web/AGENTS.md", Content: "Never edit generated files."},
				},
			},
			want: `You are an agent. Work the task through to completion with the tools you have, and stop when it is done rather than asking what to do next.

## Instructions

### AGENTS.md

Run make test.

### web/AGENTS.md

Never edit generated files.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := prompt.Default(context.Background(), tt.env)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultNeverMentionsResolvedAgents(t *testing.T) {
	// No tool runs an agent definition, so a list of them invites the
	// model to hallucinate one. The harness hands them to the builder
	// like directory-bound instructions, and the default builder writes
	// nothing for them.
	env := &prompt.Env{
		Skills: []def.Skill{{Name: "commit", Description: "Writes a commit message."}},
		Agents: []def.Agent{{Name: "reviewer", Description: "Reviews a diff for defects."}},
	}

	got, err := prompt.Default(context.Background(), env)
	require.NoError(t, err)
	assert.NotContains(t, got, "reviewer")
	assert.NotContains(t, got, "Subagents")
	assert.Contains(t, got, "commit", "skills still render")
}

func TestNoSeed(t *testing.T) {
	entries, err := prompt.NoSeed(context.Background(), &prompt.Env{WorkDir: "/work"})
	require.NoError(t, err)
	assert.Empty(t, entries, "NoSeed seeds nothing")
}

func TestDefaultSeed(t *testing.T) {
	entries, err := prompt.DefaultSeed(context.Background(), &prompt.Env{WorkDir: "/work"})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	me, ok := session.AsMessageEntry(entries[0])
	require.True(t, ok)
	assert.True(t, me.Meta, "seeded context is model-visible and transcript-hidden")

	text := me.Text()
	assert.Contains(t, text, "<environment>")
	assert.Contains(t, text, "Working directory: /work")
	assert.Contains(t, text, "Platform: "+runtime.GOOS)
	assert.Contains(t, text, "Date: "+time.Now().Format("2006-01-02"))
}
