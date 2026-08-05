package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
)

type runEventMsg struct {
	event agent.Event
	err   error
	done  bool
	next  <-chan runEventMsg
}

type chatModel struct {
	ctx            context.Context
	agent          agent.Agent
	modelName      string
	input          textarea.Model
	viewport       viewport.Model
	items          []chatItem
	tools          map[string]int
	markdown       map[int]*glamour.TermRenderer
	assistantIndex int
	width          int
	height         int
	running        bool
	cancel         context.CancelFunc
	usage          ai.Usage
	err            error
}

func newChatModel(ctx context.Context, a agent.Agent, modelName string) *chatModel {
	input := textarea.New()
	input.Placeholder = "Ask pi anything"
	input.Prompt = "❯ "
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetHeight(1)
	input.SetWidth(76)
	input.Focus()

	view := viewport.New(80, 18)
	view.MouseWheelEnabled = true
	view.MouseWheelDelta = 3

	return &chatModel{
		ctx:            ctx,
		agent:          a,
		modelName:      modelName,
		input:          input,
		viewport:       view,
		tools:          make(map[string]int),
		markdown:       make(map[int]*glamour.TermRenderer),
		assistantIndex: -1,
		width:          80,
		height:         24,
	}
}

func (m *chatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m *chatModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.refreshTranscript()
		return m, nil

	case runEventMsg:
		if msg.err != nil {
			m.err = msg.err
			m.finishRun()
			m.refreshTranscript()
			return m, nil
		}
		if !msg.done {
			m.applyAgentEvent(msg.event)
			m.refreshTranscript()
			return m, waitForRunEvent(msg.next)
		}
		m.finishRun()
		m.refreshTranscript()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.running {
				m.cancelRun()
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.running {
				m.cancelRun()
				return m, nil
			}
		case "ctrl+d":
			if !m.running && m.input.Value() == "" {
				return m, tea.Quit
			}
		case "enter":
			if !m.running {
				prompt := strings.TrimSpace(m.input.Value())
				if prompt != "" {
					return m, m.submit(prompt)
				}
			}
		}
	}

	var commands []tea.Cmd
	m.viewport, _ = m.viewport.Update(message)
	if !m.running {
		var command tea.Cmd
		m.input, command = m.input.Update(message)
		commands = append(commands, command)
		m.layout()
	}
	return m, tea.Batch(commands...)
}

func (m *chatModel) View() string {
	width := max(1, m.width)
	header := headerStyle.Render("pi") + "  " + m.modelName
	header = lipgloss.NewStyle().Width(width).Render(header)

	inputFrame := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}).
		Padding(0, 1)
	frameWidth := max(1, width-inputFrame.GetHorizontalFrameSize())
	input := inputFrame.Width(frameWidth).Render(m.input.View())

	status := "enter send  •  ctrl+c quit"
	if m.running {
		status = "working…  •  esc cancel"
	} else if m.err != nil {
		status = errorStyle.Render(m.err.Error())
	} else if totalTokens(m.usage) > 0 {
		status = fmt.Sprintf(
			"in %d  out %d  total %d",
			m.usage.Input,
			m.usage.Output,
			totalTokens(m.usage),
		)
	}
	status = lipgloss.NewStyle().
		Faint(true).
		Width(width).
		Render(status)

	return strings.Join(
		[]string{
			header,
			m.viewport.View(),
			input,
			status,
		},
		"\n",
	)
}

func (m *chatModel) appendUserMessage(prompt string) {
	message := ai.UserMessage(prompt)
	m.items = append(m.items, chatItem{
		kind:    itemMessage,
		message: &message,
	})
}

func (m *chatModel) submit(prompt string) tea.Cmd {
	m.appendUserMessage(prompt)
	m.input.Reset()
	m.input.Blur()
	m.err = nil
	m.running = true
	runCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.refreshTranscript()
	return startAgentRun(runCtx, m.agent, prompt)
}

func (m *chatModel) applyAgentEvent(event agent.Event) {
	switch event.Type {
	case agent.EventAgentEnd:
		m.usage = event.Usage
	case agent.EventMessageStart:
		m.startMessage(event.Message)
	case agent.EventMessageUpdate:
		m.updateAssistant(event.Message)
	case agent.EventMessageEnd:
		m.endMessage(event.Message)
	case agent.EventToolExecutionStart:
		tool := m.ensureTool(event.ToolCallID, event.ToolName)
		tool.args = event.Args
		tool.status = toolRunning
	case agent.EventToolExecutionUpdate:
		tool := m.ensureTool(event.ToolCallID, event.ToolName)
		tool.partial = event.PartialResult
		tool.status = toolRunning
	case agent.EventToolExecutionEnd:
		tool := m.ensureTool(event.ToolCallID, event.ToolName)
		tool.partial = nil
		tool.result = event.Result
		tool.status = toolDone
		if event.IsError {
			tool.status = toolError
		}
	}
}

func (m *chatModel) startMessage(message *ai.Message) {
	if message == nil {
		return
	}
	if message.Role == ai.RoleAssistant {
		copy := *message
		m.items = append(m.items, chatItem{
			kind:    itemMessage,
			message: &copy,
		})
		m.assistantIndex = len(m.items) - 1
		m.syncToolCalls(&copy)
		return
	}
	if message.Role == ai.RoleUser {
		copy := *message
		m.items = append(m.items, chatItem{
			kind:    itemMessage,
			message: &copy,
		})
	}
}

func (m *chatModel) updateAssistant(message *ai.Message) {
	if message == nil || message.Role != ai.RoleAssistant {
		return
	}
	if m.assistantIndex < 0 {
		m.startMessage(message)
		return
	}
	copy := *message
	m.items[m.assistantIndex].message = &copy
	m.syncToolCalls(&copy)
}

func (m *chatModel) endMessage(message *ai.Message) {
	if message == nil || message.Role != ai.RoleAssistant {
		return
	}
	m.updateAssistant(message)
	m.assistantIndex = -1
}

func (m *chatModel) syncToolCalls(message *ai.Message) {
	for _, call := range message.ToolCalls() {
		if call.Server {
			continue
		}
		tool := m.ensureTool(call.ID, call.Name)
		tool.args = call.Arguments
	}
}

func (m *chatModel) ensureTool(id, name string) *toolItem {
	if index, ok := m.tools[id]; ok {
		tool := m.items[index].tool
		if name != "" {
			tool.name = name
		}
		return tool
	}

	tool := &toolItem{
		id:     id,
		name:   name,
		status: toolPending,
	}
	m.items = append(m.items, chatItem{
		kind: itemTool,
		tool: tool,
	})
	m.tools[id] = len(m.items) - 1
	return tool
}

func (m *chatModel) layout() {
	inputFrameHeight := lipgloss.Height(
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1).
			Render(m.input.View()),
	)
	viewportHeight := max(1, m.height-inputFrameHeight-2)
	m.viewport.Width = max(1, m.width)
	m.viewport.Height = viewportHeight
	m.input.SetWidth(max(1, m.width-4))
}

func (m *chatModel) refreshTranscript() {
	wasAtBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscript(m.viewport.Width))
	if wasAtBottom || m.running {
		m.viewport.GotoBottom()
	}
}

func (m *chatModel) finishRun() {
	m.running = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.input.Focus()
}

func (m *chatModel) cancelRun() {
	if m.cancel != nil {
		m.cancel()
	}
}

func startAgentRun(ctx context.Context, a agent.Agent, prompt string) tea.Cmd {
	return func() tea.Msg {
		messages := make(chan runEventMsg)
		go func() {
			defer close(messages)
			if a == nil {
				messages <- runEventMsg{err: fmt.Errorf("agent is not configured")}
				return
			}
			stream := a.Run(ctx, ai.UserMessage(prompt))
			for event, err := range stream.Events() {
				messages <- runEventMsg{
					event: event,
					err:   err,
				}
			}
		}()
		return receiveRunEvent(messages)
	}
}

func waitForRunEvent(messages <-chan runEventMsg) tea.Cmd {
	return func() tea.Msg {
		return receiveRunEvent(messages)
	}
}

func receiveRunEvent(messages <-chan runEventMsg) runEventMsg {
	message, ok := <-messages
	if !ok {
		return runEventMsg{done: true}
	}
	message.next = messages
	return message
}

func runTUI(ctx context.Context, a agent.Agent, modelName string) error {
	model := newChatModel(ctx, a, modelName)
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithMouseCellMotion(),
	)
	_, err := program.Run()
	return err
}
