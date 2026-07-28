package demo

import "github.com/sonnes/pi-go/pkg/ai"

// DefaultScript is what the model "says" in scripted mode.
//
// Turns are consumed one per model call, so the first two make a single
// run: the model asks for the tool, then answers with its result. The
// later turns cover a second run after a branch, and whatever the
// compaction summarizer asks for. The last turn repeats once the script
// runs out, so a visitor cannot click past the end.
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
