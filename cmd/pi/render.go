package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/sonnes/pi-go/pkg/ai"
)

type itemKind int

const (
	itemMessage itemKind = iota
	itemTool
)

type toolStatus int

const (
	toolPending toolStatus = iota
	toolRunning
	toolDone
	toolError
)

type chatItem struct {
	kind    itemKind
	message *ai.Message
	tool    *toolItem
}

type toolItem struct {
	id      string
	name    string
	args    map[string]any
	partial any
	result  any
	status  toolStatus
}

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#5B21B6", Dark: "#C4B5FD"})
	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#111827", Dark: "#F9FAFB"}).
			Background(lipgloss.AdaptiveColor{Light: "#E5E7EB", Dark: "#303030"}).
			Padding(0, 1)
	thinkingStyle = lipgloss.NewStyle().
			Faint(true).
			Italic(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#4B5563", Dark: "#9CA3AF"})
	toolPendingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#92400E", Dark: "#FBBF24"})
	toolDoneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#166534", Dark: "#4ADE80"})
	toolErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#991B1B", Dark: "#F87171"})
	toolBodyStyle = lipgloss.NewStyle().
			Faint(true).
			PaddingLeft(2)
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#991B1B", Dark: "#F87171"})
)

func (m *chatModel) renderTranscript(width int) string {
	if width <= 0 {
		return ""
	}

	parts := make([]string, 0, len(m.items))
	for i := range m.items {
		item := &m.items[i]
		switch item.kind {
		case itemMessage:
			if rendered := m.renderMessage(item.message, width); rendered != "" {
				parts = append(parts, rendered)
			}
		case itemTool:
			if item.tool != nil {
				parts = append(parts, renderTool(item.tool))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func (m *chatModel) renderMessage(message *ai.Message, width int) string {
	if message == nil {
		return ""
	}
	if message.Role == ai.RoleUser {
		return userStyle.Render(message.Text())
	}
	if message.Role != ai.RoleAssistant {
		return ""
	}

	var parts []string
	for _, content := range message.Content {
		switch block := content.(type) {
		case ai.Text:
			if text := strings.TrimSpace(block.Text); text != "" {
				parts = append(parts, m.renderMarkdown(text, width))
			}
		case ai.Thinking:
			if thinking := strings.TrimSpace(block.Thinking); thinking != "" {
				parts = append(parts, thinkingStyle.Render(thinking))
			}
		case ai.ToolCall:
			if block.Server {
				parts = append(parts, renderServerTool(block))
			}
		}
	}

	switch message.StopReason {
	case ai.StopReasonLength:
		parts = append(parts, errorStyle.Render("Response stopped at the output token limit."))
	case ai.StopReasonError:
		text := message.Error
		if text == "" {
			text = "The model returned an error."
		}
		parts = append(parts, errorStyle.Render(text))
	case ai.StopReasonAborted:
		parts = append(parts, errorStyle.Render("Canceled"))
	}

	return strings.Join(parts, "\n\n")
}

func (m *chatModel) renderMarkdown(text string, width int) string {
	wrap := max(20, width-2)
	renderer := m.markdown[wrap]
	if renderer == nil {
		style := "light"
		if lipgloss.HasDarkBackground() {
			style = "dark"
		}
		var err error
		renderer, err = glamour.NewTermRenderer(
			glamour.WithStandardStyle(style),
			glamour.WithWordWrap(wrap),
		)
		if err != nil {
			return text
		}
		m.markdown[wrap] = renderer
	}

	rendered, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return strings.Trim(rendered, "\n")
}

func renderTool(tool *toolItem) string {
	icon := "●"
	style := toolPendingStyle
	switch tool.status {
	case toolDone:
		icon = "✓"
		style = toolDoneStyle
	case toolError:
		icon = "✗"
		style = toolErrorStyle
	}

	lines := []string{style.Render(icon + " " + tool.name)}
	if len(tool.args) > 0 {
		lines = append(lines, toolBodyStyle.Render(truncateRunes(formatJSON(tool.args), 240)))
	}
	if tool.result != nil {
		lines = append(lines, toolBodyStyle.Render(truncateRunes(formatToolValue(tool.result), 600)))
	} else if tool.partial != nil {
		lines = append(lines, toolBodyStyle.Render(truncateRunes(formatToolValue(tool.partial), 240)))
	}
	return strings.Join(lines, "\n")
}

func renderServerTool(call ai.ToolCall) string {
	name := string(call.ServerType)
	if name == "" {
		name = call.Name
	}
	tool := &toolItem{
		id:     call.ID,
		name:   name,
		args:   call.Arguments,
		status: toolRunning,
	}
	if call.Output != nil {
		tool.result = call.Output.Content
		tool.status = toolDone
		if call.Output.IsError {
			tool.status = toolError
		}
	}
	return renderTool(tool)
}

func formatJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func formatToolValue(value any) string {
	switch result := value.(type) {
	case ai.ToolResult:
		if result.Content != "" {
			return result.Content
		}
		if result.Type != "" {
			return result.Type
		}
	case *ai.ToolResult:
		if result != nil {
			return formatToolValue(*result)
		}
	case string:
		return result
	}
	return formatJSON(value)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
