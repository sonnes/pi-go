package main

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

func TestChatModel_MessageUpdateUsesAccumulatedSnapshot(t *testing.T) {
	m := newChatModel(
		context.Background(),
		nil,
		"test/model",
	)
	m.appendUserMessage("hello")
	m.applyAgentEvent(agent.Event{
		Type:    agent.EventMessageStart,
		Message: &ai.Message{Role: ai.RoleAssistant},
	})
	m.applyAgentEvent(agent.Event{
		Type: agent.EventMessageUpdate,
		Message: &ai.Message{
			Role: ai.RoleAssistant,
			Content: []ai.Content{
				ai.Text{Text: "**rendered answer**"},
			},
		},
		AssistantEvent: &ai.Event{
			Type:  ai.EventToolDelta,
			Delta: `{"unrendered":"tool delta"}`,
		},
	})

	output := ansi.Strip(m.renderTranscript(80))
	assert.Contains(t, output, "hello")
	assert.Contains(t, output, "rendered answer")
	assert.NotContains(t, output, "**")
	assert.NotContains(t, output, "unrendered")
}

func TestChatModel_ToolLifecycleUpdatesOneItem(t *testing.T) {
	m := newChatModel(
		context.Background(),
		nil,
		"test/model",
	)
	m.applyAgentEvent(agent.Event{
		Type:       agent.EventToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "read_file",
		Args:       map[string]any{"path": "README.md"},
	})
	m.applyAgentEvent(agent.Event{
		Type:          agent.EventToolExecutionUpdate,
		ToolCallID:    "call-1",
		ToolName:      "read_file",
		PartialResult: "halfway",
	})
	m.applyAgentEvent(agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "read_file",
		Result:     ai.NewTextResult("call-1", "file contents"),
	})

	require.Len(t, m.items, 1)
	require.NotNil(t, m.items[0].tool)
	assert.Equal(t, toolDone, m.items[0].tool.status)

	output := ansi.Strip(m.renderTranscript(80))
	assert.Contains(t, output, "read_file")
	assert.Contains(t, output, "README.md")
	assert.Contains(t, output, "file contents")
	assert.NotContains(t, output, "halfway")
}

func TestChatModel_ViewShowsModelAndHelp(t *testing.T) {
	m := newChatModel(
		context.Background(),
		nil,
		"test/model",
	)
	_, _ = m.Update(tea.WindowSizeMsg{
		Width:  80,
		Height: 24,
	})

	view := m.View()
	assert.Contains(t, ansi.Strip(view), "test/model")
	assert.Contains(t, ansi.Strip(view), "enter send")
}

func TestChatModel_InputStartsFocused(t *testing.T) {
	m := newChatModel(
		context.Background(),
		nil,
		"test/model",
	)

	assert.True(t, m.input.Focused())
}
