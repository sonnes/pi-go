package agent

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvent_MarshalJSON_SessionInit(t *testing.T) {
	tests := []struct {
		name      string
		event     Event
		wantKeys  []string
		wantPairs map[string]any
	}{
		{
			name: "with session ID",
			event: Event{
				Type:      EventSessionInit,
				SessionID: "sess-7",
			},
			wantKeys: []string{"type", "session_id"},
			wantPairs: map[string]any{
				"type":       "session_init",
				"session_id": "sess-7",
			},
		},
		{
			name: "without session ID omits field",
			event: Event{
				Type: EventSessionInit,
			},
			wantKeys:  []string{"type"},
			wantPairs: map[string]any{"type": "session_init"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.event)
			require.NoError(t, err)

			var m map[string]any
			require.NoError(t, json.Unmarshal(data, &m))

			assert.Len(t, m, len(tt.wantKeys))
			for k, v := range tt.wantPairs {
				assert.Equal(t, v, m[k])
			}
		})
	}
}

func TestEvent_MarshalJSON_SessionEnd(t *testing.T) {
	tests := []struct {
		name      string
		event     Event
		wantKeys  []string
		wantPairs map[string]any
	}{
		{
			name: "without error omits field",
			event: Event{
				Type: EventSessionEnd,
			},
			wantKeys:  []string{"type"},
			wantPairs: map[string]any{"type": "session_end"},
		},
		{
			name: "with error",
			event: Event{
				Type: EventSessionEnd,
				Err:  errors.New("subprocess died"),
			},
			wantKeys: []string{"type", "error"},
			wantPairs: map[string]any{
				"type":  "session_end",
				"error": "subprocess died",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.event)
			require.NoError(t, err)

			var m map[string]any
			require.NoError(t, json.Unmarshal(data, &m))

			assert.Len(t, m, len(tt.wantKeys))
			for k, v := range tt.wantPairs {
				assert.Equal(t, v, m[k])
			}
		})
	}
}

func TestEvent_MarshalJSON_AgentStart(t *testing.T) {
	tests := []struct {
		name      string
		event     Event
		wantKeys  []string
		wantPairs map[string]any
	}{
		{
			name: "with session ID",
			event: Event{
				Type:      EventAgentStart,
				SessionID: "sess-42",
			},
			wantKeys: []string{"type", "session_id"},
			wantPairs: map[string]any{
				"type":       "agent_start",
				"session_id": "sess-42",
			},
		},
		{
			name: "without session ID omits field",
			event: Event{
				Type: EventAgentStart,
			},
			wantKeys:  []string{"type"},
			wantPairs: map[string]any{"type": "agent_start"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.event)
			require.NoError(t, err)

			var m map[string]any
			require.NoError(t, json.Unmarshal(data, &m))

			assert.Len(t, m, len(tt.wantKeys))
			for k, v := range tt.wantPairs {
				assert.Equal(t, v, m[k])
			}
		})
	}
}
