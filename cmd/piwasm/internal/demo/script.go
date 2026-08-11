package demo

import "github.com/sonnes/pi-go/pkg/ai"

// DefaultScript is what the model "says" in scripted mode.
//
// Each model call consumes one turn, so the first two turns make a single
// run. The model asks for the tool, then answers with the result. The
// later turns cover a second run after a branch, and the request from the
// compaction summarizer. When the script runs out, the last turn repeats.
// A visitor therefore cannot click past the end.
func DefaultScript() []Turn {
	return []Turn{
		{
			Text: "Let me check that.",
			Tool: &ai.ToolCall{
				ID:        "call_weather_1",
				Name:      "get_weather",
				Arguments: map[string]any{"city": "Paris"},
			},
		},
		{Text: "It's 22°C and clear in Paris right now."},
		{
			Text: "Checking that one too.",
			Tool: &ai.ToolCall{
				ID:        "call_weather_2",
				Name:      "get_weather",
				Arguments: map[string]any{"city": "Rome"},
			},
		},
		{Text: "Rome is 22°C and clear as well."},
		{Text: "The visitor asked about the weather in two cities; the tool answered 22°C and clear for both."},
		{Text: "Same as before — 22°C and clear."},
	}
}
