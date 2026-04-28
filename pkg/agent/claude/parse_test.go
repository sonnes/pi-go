package claude

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    rawLine
		wantErr bool
	}{
		{
			name:  "system init",
			input: `{"type":"system","subtype":"init","session_id":"sess-123"}`,
			want: rawLine{
				Type:      "system",
				Subtype:   "init",
				SessionID: "sess-123",
			},
		},
		{
			name:  "result success",
			input: `{"type":"result","subtype":"success","result":"Hello!","session_id":"sess-123","cost_usd":0.005,"usage":{"input_tokens":100,"output_tokens":50}}`,
			want: rawLine{
				Type:      "result",
				Subtype:   "success",
				Result:    "Hello!",
				SessionID: "sess-123",
				CostUSD:   0.005,
				Usage: &anthropic.Usage{
					InputTokens:  100,
					OutputTokens: 50,
				},
			},
		},
		{
			name:  "result with cache tokens",
			input: `{"type":"result","subtype":"success","result":"Hi","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":200,"cache_creation_input_tokens":300}}`,
			want: rawLine{
				Type:    "result",
				Subtype: "success",
				Result:  "Hi",
				Usage: &anthropic.Usage{
					InputTokens:              100,
					OutputTokens:             50,
					CacheReadInputTokens:     200,
					CacheCreationInputTokens: 300,
				},
			},
		},
		{
			name:    "malformed JSON",
			input:   `{not json}`,
			wantErr: true,
		},
		{
			name:  "empty object",
			input: `{}`,
			want:  rawLine{},
		},
		{
			name:  "unknown type",
			input: `{"type":"unknown_future_type"}`,
			want:  rawLine{Type: "unknown_future_type"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLine([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.Type, got.Type)
			assert.Equal(t, tt.want.Subtype, got.Subtype)
			assert.Equal(t, tt.want.SessionID, got.SessionID)
			assert.Equal(t, tt.want.Result, got.Result)
			assert.InDelta(t, tt.want.CostUSD, got.CostUSD, 0.0001)
			if tt.want.Usage != nil {
				require.NotNil(t, got.Usage)
				assert.Equal(t, tt.want.Usage.InputTokens, got.Usage.InputTokens)
				assert.Equal(t, tt.want.Usage.OutputTokens, got.Usage.OutputTokens)
				assert.Equal(t, tt.want.Usage.CacheReadInputTokens, got.Usage.CacheReadInputTokens)
				assert.Equal(t, tt.want.Usage.CacheCreationInputTokens, got.Usage.CacheCreationInputTokens)
			}
		})
	}
}

func TestToAIMessage(t *testing.T) {
	tests := []struct {
		name    string
		rawJSON string
		want    ai.Message
	}{
		{
			name: "text only",
			rawJSON: `{
				"role":"assistant",
				"content":[{"type":"text","text":"Hello world"}],
				"stop_reason":"end_turn"
			}`,
			want: ai.Message{
				Role:       ai.RoleAssistant,
				Content:    []ai.Content{ai.Text{Text: "Hello world"}},
				StopReason: ai.StopReasonStop,
			},
		},
		{
			name: "tool use",
			rawJSON: `{
				"role":"assistant",
				"content":[
					{"type":"text","text":"Let me read that file."},
					{"type":"tool_use","id":"tool_1","name":"Read","input":{"file_path":"/tmp/foo.go"}}
				],
				"stop_reason":"tool_use"
			}`,
			want: ai.Message{
				Role: ai.RoleAssistant,
				Content: []ai.Content{
					ai.Text{Text: "Let me read that file."},
					ai.ToolCall{
						ID:        "tool_1",
						Name:      "Read",
						Arguments: map[string]any{"file_path": "/tmp/foo.go"},
					},
				},
				StopReason: ai.StopReasonToolUse,
			},
		},
		{
			name: "with usage",
			rawJSON: `{
				"role":"assistant",
				"content":[{"type":"text","text":"Hi"}],
				"stop_reason":"end_turn",
				"usage":{
					"input_tokens":100,
					"output_tokens":50,
					"cache_read_input_tokens":200,
					"cache_creation_input_tokens":300
				}
			}`,
			want: ai.Message{
				Role:       ai.RoleAssistant,
				Content:    []ai.Content{ai.Text{Text: "Hi"}},
				StopReason: ai.StopReasonStop,
				Usage: ai.Usage{
					Input:      100,
					Output:     50,
					CacheRead:  200,
					CacheWrite: 300,
					Total:      150,
				},
			},
		},
		{
			name: "thinking content",
			rawJSON: `{
				"role":"assistant",
				"content":[
					{"type":"thinking","thinking":"Let me think..."},
					{"type":"text","text":"The answer is 42."}
				],
				"stop_reason":"end_turn"
			}`,
			want: ai.Message{
				Role: ai.RoleAssistant,
				Content: []ai.Content{
					ai.Thinking{Thinking: "Let me think..."},
					ai.Text{Text: "The answer is 42."},
				},
				StopReason: ai.StopReasonStop,
			},
		},
		{
			name: "unknown content type skipped",
			rawJSON: `{
				"role":"assistant",
				"content":[
					{"type":"text","text":"Hello"},
					{"type":"server_tool_use"}
				],
				"stop_reason":"end_turn"
			}`,
			want: ai.Message{
				Role:       ai.RoleAssistant,
				Content:    []ai.Content{ai.Text{Text: "Hello"}},
				StopReason: ai.StopReasonStop,
			},
		},
		{
			name:    "empty message",
			rawJSON: `{"role":"assistant","content":[],"stop_reason":"end_turn"}`,
			want: ai.Message{
				Role:       ai.RoleAssistant,
				StopReason: ai.StopReasonStop,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg anthropic.Message
			require.NoError(t, json.Unmarshal([]byte(tt.rawJSON), &msg))

			got := toAIMessage(msg)
			assert.Equal(t, tt.want.Role, got.Role)
			assert.Equal(t, tt.want.StopReason, got.StopReason)
			assert.Equal(t, tt.want.Usage, got.Usage)
			require.Len(t, got.Content, len(tt.want.Content))
			for i, wantC := range tt.want.Content {
				assert.Equal(t, wantC, got.Content[i])
			}
		})
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		input string
		want  ai.StopReason
	}{
		{"end_turn", ai.StopReasonStop},
		{"stop_sequence", ai.StopReasonStop},
		{"tool_use", ai.StopReasonToolUse},
		{"max_tokens", ai.StopReasonLength},
		{"unknown", ai.StopReasonStop},
		{"", ai.StopReasonStop},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, mapStopReason(tt.input))
		})
	}
}

func TestMapper_HandleLine(t *testing.T) {
	tests := []struct {
		name   string
		lines  []rawLine
		events []agent.EventType
	}{
		{
			name: "simple text response",
			lines: []rawLine{
				{Type: "system", Subtype: "init", SessionID: "s1"},
				makeAssistantLine(t, "Hello!", "end_turn"),
				{Type: "result", Subtype: "success", Result: "Hello!"},
			},
			// system/init is filtered by readLoop and produces no
			// parser events — runTurn emits AgentStart explicitly.
			events: []agent.EventType{
				agent.EventTurnStart,
				agent.EventMessageStart,
				agent.EventMessageEnd,
				agent.EventTurnEnd,
			},
		},
		{
			name: "multi-turn with tool use",
			lines: []rawLine{
				{Type: "system", Subtype: "init", SessionID: "s1"},
				makeAssistantWithToolLine(t, "Let me check.", "Read", "tool_use"),
				makeAssistantLine(t, "The file contains Go code.", "end_turn"),
				{Type: "result", Subtype: "success", Result: "The file contains Go code."},
			},
			events: []agent.EventType{
				// Turn 1: assistant with tool_use (turn stays open)
				agent.EventTurnStart,
				agent.EventMessageStart,
				agent.EventMessageEnd,
				agent.EventToolExecutionStart,
				// Next assistant closes turn 1, opens turn 2
				agent.EventTurnEnd,
				agent.EventTurnStart,
				agent.EventMessageStart,
				agent.EventMessageEnd,
				agent.EventTurnEnd,
			},
		},
		{
			name: "system init produces no parser events",
			lines: []rawLine{
				{Type: "system", Subtype: "init", SessionID: "s1"},
			},
			events: nil,
		},
		{
			name: "unknown line type skipped",
			lines: []rawLine{
				{Type: "system", Subtype: "init", SessionID: "s1"},
				{Type: "unknown_type"},
				makeAssistantLine(t, "Hello!", "end_turn"),
				{Type: "result", Subtype: "success", Result: "Hello!"},
			},
			events: []agent.EventType{
				agent.EventTurnStart,
				agent.EventMessageStart,
				agent.EventMessageEnd,
				agent.EventTurnEnd,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &parser{}
			var gotEvents []agent.Event
			for _, line := range tt.lines {
				gotEvents = append(gotEvents, m.handleLine(line)...)
			}

			var gotTypes []agent.EventType
			for _, e := range gotEvents {
				gotTypes = append(gotTypes, e.Type)
			}
			assert.Equal(t, tt.events, gotTypes)
		})
	}
}

// system/init is consumed by [Agent.readLoop] (which captures
// SessionID and skips parser dispatch). The parser itself produces no
// events for that line — runTurn emits AgentStart explicitly per Send.
func TestMapper_SystemInitNoEvents(t *testing.T) {
	m := &parser{}
	events := m.handleLine(rawLine{Type: "system", Subtype: "init", SessionID: "sess-abc"})

	assert.Empty(t, events)
}

func TestMapper_Usage(t *testing.T) {
	m := &parser{}
	m.handleLine(rawLine{Type: "system", Subtype: "init", SessionID: "s1"})
	m.handleLine(makeAssistantLine(t, "Hello!", "end_turn"))
	m.handleLine(rawLine{
		Type:    "result",
		Subtype: "success",
		Result:  "Hello!",
		Usage: &anthropic.Usage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	})

	assert.Equal(t, 100, m.usage.Input)
	assert.Equal(t, 50, m.usage.Output)
	assert.Equal(t, 150, m.usage.Total)
}

func TestMapper_Messages(t *testing.T) {
	m := &parser{}
	m.handleLine(rawLine{Type: "system", Subtype: "init", SessionID: "s1"})
	m.handleLine(makeAssistantLine(t, "Hello!", "end_turn"))
	m.handleLine(rawLine{Type: "result", Subtype: "success", Result: "Hello!"})

	require.Len(t, m.messages, 1)
	assert.Equal(t, ai.RoleAssistant, m.messages[0].Role)
	assert.Equal(t, "Hello!", m.messages[0].Text())
}

func TestMapper_ToolExecutionEvents(t *testing.T) {
	m := &parser{}
	events := m.handleLine(makeAssistantWithToolLine(t, "Let me check.", "Read", "tool_use"))

	types := make([]agent.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	// Turn stays open — no TurnEnd yet (waiting for tool results).
	assert.Equal(t, []agent.EventType{
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventToolExecutionStart,
	}, types)
	assert.True(t, m.inTurn, "turn should stay open after tool call")

	// Verify tool execution start has the right fields.
	toolEvt := events[3]
	assert.Equal(t, "t1", toolEvt.ToolCallID)
	assert.Equal(t, "Read", toolEvt.ToolName)
}

func TestMapper_UserToolResults(t *testing.T) {
	m := &parser{}

	// First an assistant with tool_use.
	m.handleLine(makeAssistantWithToolLine(t, "Reading.", "Read", "tool_use"))

	// Then a user message with tool_result.
	userLine := makeUserToolResultLine(t, "t1", "file contents here")
	events := m.handleLine(userLine)

	types := make([]agent.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	assert.Equal(t, []agent.EventType{
		agent.EventToolExecutionEnd,
		agent.EventMessageStart,
		agent.EventMessageEnd,
	}, types)

	// Verify tool execution end fields.
	assert.Equal(t, "t1", events[0].ToolCallID)
	assert.Equal(t, "file contents here", events[0].Result)

	// Verify tool result message accumulated.
	require.Len(t, m.toolResults, 1)
	assert.Equal(t, ai.RoleToolResult, m.toolResults[0].Role)
	assert.Equal(t, "t1", m.toolResults[0].ToolCallID)
}

func TestMapper_TurnEndCarriesToolResults(t *testing.T) {
	m := &parser{}
	var allEvents []agent.Event

	// assistant → tool result → final assistant → result
	allEvents = append(allEvents,
		m.handleLine(makeAssistantWithToolLine(t, "Checking.", "Bash", "tool_use"))...)
	allEvents = append(allEvents,
		m.handleLine(makeUserToolResultLine(t, "t1", "ok"))...)
	allEvents = append(allEvents,
		m.handleLine(makeAssistantLine(t, "Done.", "end_turn"))...)
	allEvents = append(allEvents,
		m.handleLine(rawLine{
			Type:    "result",
			Subtype: "success",
			Result:  "Done.",
		})...)

	// The first TurnEnd closes the tool-use turn and carries
	// the tool results. The second TurnEnd is for the final
	// assistant response (no tools).
	var turnEnds []agent.Event
	for _, e := range allEvents {
		if e.Type == agent.EventTurnEnd {
			turnEnds = append(turnEnds, e)
		}
	}
	require.Len(t, turnEnds, 2)
	require.Len(t, turnEnds[0].ToolResults, 1, "first turn_end should carry tool results")
	assert.Equal(t, "t1", turnEnds[0].ToolResults[0].ToolCallID)
	assert.Empty(t, turnEnds[1].ToolResults, "second turn_end has no tools")
}

func TestMapper_ResultIsError(t *testing.T) {
	m := &parser{}
	events := m.handleLine(rawLine{
		Type:    "result",
		Subtype: "error",
		Result:  "Rate limited",
		IsError: true,
	})

	// Should return no events (error is on parser.err).
	assert.Empty(t, events)
	require.Error(t, m.err)
	assert.Contains(t, m.err.Error(), "Rate limited")
}

func TestMapper_ResultDeduplication(t *testing.T) {
	m := &parser{}

	// Assistant says "Hello!" first.
	m.handleLine(makeAssistantLine(t, "Hello!", "end_turn"))
	require.Len(t, m.messages, 1)

	// Result also carries "Hello!" — should NOT create another message.
	m.handleLine(rawLine{
		Type:    "result",
		Subtype: "success",
		Result:  "Hello!",
	})
	assert.Len(t, m.messages, 1, "should not duplicate result text")
}

func TestMapper_ResultNewText(t *testing.T) {
	m := &parser{}

	// No prior assistant message.
	events := m.handleLine(rawLine{
		Type:    "result",
		Subtype: "success",
		Result:  "Quick answer.",
	})

	// Should create a message from result text.
	require.Len(t, m.messages, 1)
	assert.Equal(t, "Quick answer.", m.messages[0].Text())

	// Should emit message start/end events.
	types := make([]agent.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	assert.Contains(t, types, agent.EventMessageStart)
	assert.Contains(t, types, agent.EventMessageEnd)
}

func TestMapper_ResultCostUSD(t *testing.T) {
	m := &parser{}
	m.handleLine(rawLine{
		Type:    "result",
		Subtype: "success",
		CostUSD: 0.0042,
		Usage: &anthropic.Usage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	})

	assert.InDelta(t, 0.0042, m.usage.Cost.Total, 0.0001)
}

func TestMapper_FullConversationWithTools(t *testing.T) {
	m := &parser{}
	var allEvents []agent.Event
	collect := func(line rawLine) {
		allEvents = append(allEvents, m.handleLine(line)...)
	}

	collect(rawLine{Type: "system", Subtype: "init", SessionID: "sess-1"})
	collect(makeAssistantWithToolLine(t, "Let me read.", "Read", "tool_use"))
	collect(makeUserToolResultLine(t, "t1", "package main"))
	collect(makeAssistantLine(t, "It's a Go file.", "end_turn"))
	collect(rawLine{
		Type:    "result",
		Subtype: "success",
		Result:  "It's a Go file.",
		Usage:   &anthropic.Usage{InputTokens: 500, OutputTokens: 50},
		CostUSD: 0.001,
	})

	types := make([]agent.EventType, len(allEvents))
	for i, e := range allEvents {
		types[i] = e.Type
	}

	assert.Equal(t, []agent.EventType{
		// system/init is filtered by readLoop, not the parser.
		// Turn 1: assistant calls tool, turn stays open for results
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventToolExecutionStart,
		// Tool result arrives inside the same turn
		agent.EventToolExecutionEnd,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		// Turn 1 closes (next assistant triggers close)
		agent.EventTurnEnd,
		// Turn 2: final assistant
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventMessageEnd,
		agent.EventTurnEnd,
		// Result (deduplicated — no extra message)
	}, types)

	require.Len(t, m.messages, 3) // assistant + tool_result + assistant
	assert.Equal(t, ai.RoleAssistant, m.messages[0].Role)
	assert.Equal(t, ai.RoleToolResult, m.messages[1].Role)
	assert.Equal(t, ai.RoleAssistant, m.messages[2].Role)
	assert.InDelta(t, 0.001, m.usage.Cost.Total, 0.0001)
}

func TestMapper_UserToolResultArrayContent(t *testing.T) {
	// Real Claude Code emits tool_result.content as an array of
	// {type:"text", text:"..."} objects, not a plain string.
	m := &parser{}

	line, err := parseLine([]byte(`{
		"type": "user",
		"message": {"content": [{
			"type": "tool_result",
			"tool_use_id": "t1",
			"content": [
				{"type": "text", "text": "first chunk"},
				{"type": "text", "text": " second chunk"}
			]
		}]}
	}`))
	require.NoError(t, err)

	events := m.handleLine(line)

	require.NotEmpty(t, events)
	assert.Equal(t, agent.EventToolExecutionEnd, events[0].Type)
	assert.Equal(t, "first chunk second chunk", events[0].Result)

	require.Len(t, m.toolResults, 1)
	assert.Equal(t, "first chunk second chunk", m.toolResults[0].Text())
}

func TestMapper_UserToolResultStringContent(t *testing.T) {
	m := &parser{}

	line, err := parseLine([]byte(`{
		"type": "user",
		"message": {"content": [{
			"type": "tool_result",
			"tool_use_id": "t1",
			"content": "plain string result"
		}]}
	}`))
	require.NoError(t, err)

	events := m.handleLine(line)
	require.NotEmpty(t, events)
	assert.Equal(t, "plain string result", events[0].Result)
}

// --- usage-propagation regression tests ---

// TestMapper_StaleAssistantUsageDropped verifies that per-line usage
// on assistant lines (a streaming snapshot where output_tokens=0 until
// finalized) does not leak into emitted MessageEnd events. The final
// usage arrives on the result line and must be attached to the last
// emitted assistant MessageEnd.
func TestMapper_StaleAssistantUsageDropped(t *testing.T) {
	m := &parser{}

	// Assistant line carries stale streaming usage (output=0).
	stale := `{
		"role":"assistant",
		"content":[{"type":"text","text":"Hi!"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":3,"output_tokens":0,"cache_creation_input_tokens":5000}
	}`

	eventsA := m.handleLine(rawLine{
		Type:    "assistant",
		Message: json.RawMessage(stale),
	})
	// MessageEnd for this assistant must be buffered until the result
	// line attaches the authoritative usage.
	for _, e := range eventsA {
		assert.NotEqual(t, agent.EventMessageEnd, e.Type,
			"stop-reason assistant MessageEnd should be deferred to result line")
		assert.NotEqual(t, agent.EventTurnEnd, e.Type,
			"TurnEnd should be deferred to result line")
	}

	// Result line carries the final usage.
	eventsR := m.handleLine(rawLine{
		Type:    "result",
		Subtype: "success",
		Result:  "Hi!",
		Usage: &anthropic.Usage{
			InputTokens:              3,
			OutputTokens:             25,
			CacheCreationInputTokens: 5000,
		},
	})

	var msgEnd *agent.Event
	for i := range eventsR {
		if eventsR[i].Type == agent.EventMessageEnd {
			msgEnd = &eventsR[i]
			break
		}
	}
	require.NotNil(t, msgEnd, "flushed MessageEnd expected on result line")
	require.NotNil(t, msgEnd.Message)
	assert.Equal(t, 3, msgEnd.Message.Usage.Input)
	assert.Equal(t, 25, msgEnd.Message.Usage.Output)
	assert.Equal(t, 28, msgEnd.Message.Usage.Total)
	assert.Equal(t, 5000, msgEnd.Message.Usage.CacheWrite)
}

// TestMapper_UsageOnlyOnLastAssistantLine covers the extended-thinking
// case where the CLI emits separate assistant lines for thinking and
// text. Only the last emitted assistant MessageEnd should carry the
// turn-level usage.
func TestMapper_UsageOnlyOnLastAssistantLine(t *testing.T) {
	m := &parser{}
	var all []agent.Event

	thinking := `{
		"role":"assistant",
		"content":[{"type":"thinking","thinking":"pondering","signature":"sig1"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":3,"output_tokens":0,"cache_creation_input_tokens":5000}
	}`
	all = append(all, m.handleLine(rawLine{
		Type:    "assistant",
		Message: json.RawMessage(thinking),
	})...)

	text := `{
		"role":"assistant",
		"content":[{"type":"text","text":"The answer is 42."}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":3,"output_tokens":0,"cache_creation_input_tokens":5000}
	}`
	all = append(all, m.handleLine(rawLine{
		Type:    "assistant",
		Message: json.RawMessage(text),
	})...)

	all = append(all, m.handleLine(rawLine{
		Type:    "result",
		Subtype: "success",
		Result:  "The answer is 42.",
		Usage: &anthropic.Usage{
			InputTokens:              3,
			OutputTokens:             25,
			CacheCreationInputTokens: 5000,
		},
	})...)

	var ends []*agent.Event
	for i := range all {
		e := &all[i]
		if e.Type == agent.EventMessageEnd && e.Message != nil && e.Message.Role == ai.RoleAssistant {
			ends = append(ends, e)
		}
	}
	require.Len(t, ends, 2)

	// First (thinking) message — no usage.
	assert.Equal(t, ai.Usage{}, ends[0].Message.Usage,
		"intermediate assistant message should not carry usage")

	// Last (text) message — turn-level usage.
	assert.Equal(t, 3, ends[1].Message.Usage.Input)
	assert.Equal(t, 25, ends[1].Message.Usage.Output)
	assert.Equal(t, 28, ends[1].Message.Usage.Total)
	assert.Equal(t, 5000, ends[1].Message.Usage.CacheWrite)
}

// TestMapper_UsageOnResultCreatedMessage covers the case where the
// assistant emitted no text block and the result line synthesizes one.
// The synthesized message must receive the final usage.
func TestMapper_UsageOnResultCreatedMessage(t *testing.T) {
	m := &parser{}
	var all []agent.Event

	thinking := `{
		"role":"assistant",
		"content":[{"type":"thinking","thinking":"hmm"}],
		"stop_reason":"end_turn"
	}`
	all = append(all, m.handleLine(rawLine{
		Type:    "assistant",
		Message: json.RawMessage(thinking),
	})...)

	all = append(all, m.handleLine(rawLine{
		Type:    "result",
		Subtype: "success",
		Result:  "Answer text.",
		Usage: &anthropic.Usage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	})...)

	var last *agent.Event
	for i := range all {
		e := &all[i]
		if e.Type == agent.EventMessageEnd && e.Message != nil && e.Message.Role == ai.RoleAssistant {
			last = e
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, "Answer text.", last.Message.Text())
	assert.Equal(t, 10, last.Message.Usage.Input)
	assert.Equal(t, 5, last.Message.Usage.Output)
	assert.Equal(t, 15, last.Message.Usage.Total)
}

// --- helpers ---

// makeAssistantLine builds a type:"assistant" rawLine whose embedded
// message is the given text wrapped as an Anthropic content block.
func makeAssistantLine(t *testing.T, text, stopReason string) rawLine {
	t.Helper()
	body := fmt.Sprintf(
		`{"role":"assistant","content":[{"type":"text","text":%q}],"stop_reason":%q}`,
		text, stopReason,
	)
	return rawLine{Type: "assistant", Message: json.RawMessage(body)}
}

// makeAssistantWithToolLine builds an assistant rawLine containing both
// a text block and a tool_use block.
func makeAssistantWithToolLine(t *testing.T, text, toolName, stopReason string) rawLine {
	t.Helper()
	body := fmt.Sprintf(
		`{"role":"assistant","content":[`+
			`{"type":"text","text":%q},`+
			`{"type":"tool_use","id":"t1","name":%q,"input":{}}`+
			`],"stop_reason":%q}`,
		text, toolName, stopReason,
	)
	return rawLine{Type: "assistant", Message: json.RawMessage(body)}
}

func makeUserToolResultLine(t *testing.T, toolUseID, content string) rawLine {
	t.Helper()
	body := fmt.Sprintf(
		`{"content":[{"type":"tool_result","tool_use_id":%q,"content":%q}]}`,
		toolUseID, content,
	)
	return rawLine{Type: "user", Message: json.RawMessage(body)}
}
