package ai_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Events cross the wire in applications that stream a run to a browser,
// so their keys are a contract — snake_case, like the rest of the
// package, not Go field names.
func TestEventJSONKeys(t *testing.T) {
	evt := ai.Event{
		Type:         ai.EventTextDelta,
		ContentIndex: 1,
		Delta:        "hi",
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))

	assert.Equal(t, "text_delta", fields["type"])
	assert.Equal(t, float64(1), fields["content_index"])
	assert.Equal(t, "hi", fields["delta"])
	assert.NotContains(t, fields, "content")
	assert.NotContains(t, fields, "tool_call")
}

func TestEventRoundTrips(t *testing.T) {
	want := ai.Event{
		Type:     ai.EventToolEnd,
		Content:  "done",
		ToolCall: &ai.ToolCall{ID: "tc1", Name: "records_put"},
	}

	data, err := json.Marshal(want)
	require.NoError(t, err)

	var got ai.Event
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, want, got)
}
