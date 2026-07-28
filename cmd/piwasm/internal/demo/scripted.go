package demo

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
)

// Turn is one scripted model response: the text it streams, and
// optionally the tool it asks for.
type Turn struct {
	Text string
	Tool *ai.ToolCall
}

// Scripted is an [ai.TextProvider] that replays a fixed script instead
// of calling a model. It exists so the browser demo can run the real
// agent loop — real tool dispatch, real session tree — with no
// credentials, no network, and the same output every time.
//
// Turns are consumed in order, one per model call, so a run that ends
// in a tool call spends two of them. Once the script is exhausted the
// final turn repeats, which keeps a visitor clicking Run a fourth time
// from falling off the end.
type Scripted struct {
	// Delay paces the text deltas so streaming is visible. Zero in
	// tests, a few tens of milliseconds in the browser.
	Delay time.Duration

	turns []Turn

	mu sync.Mutex
	n  int
}

var _ ai.TextProvider = (*Scripted)(nil)

// NewScripted returns a provider that replays turns.
func NewScripted(turns []Turn) *Scripted {
	return &Scripted{turns: turns}
}

// next returns the turn for this call, repeating the last one once the
// script runs out.
func (s *Scripted) next() Turn {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.turns) == 0 {
		return Turn{}
	}

	i := min(s.n, len(s.turns)-1)
	s.n++

	return s.turns[i]
}

// Reset rewinds the script to its first turn.
func (s *Scripted) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n = 0
}

// StreamText replays the next turn as a stream of word deltas.
func (s *Scripted) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) *ai.EventStream {
	turn := s.next()

	return ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		push(ai.Event{Type: ai.EventTextStart})

		// Words carry their trailing space so a consumer that
		// concatenates every delta reconstructs the text exactly.
		for i, word := range strings.SplitAfter(turn.Text, " ") {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if i > 0 && s.Delay > 0 {
				time.Sleep(s.Delay)
			}
			push(ai.Event{Type: ai.EventTextDelta, Delta: word})
		}

		push(ai.Event{Type: ai.EventTextEnd, Content: turn.Text})

		content := []ai.Content{ai.Text{Text: turn.Text}}
		if turn.Tool != nil {
			push(ai.Event{Type: ai.EventToolEnd, ToolCall: turn.Tool})
			content = append(content, *turn.Tool)
		}

		msg := ai.AssistantMessage(content...)
		msg.StopReason = ai.StopReasonStop
		if turn.Tool != nil {
			msg.StopReason = ai.StopReasonToolUse
		}

		return &msg, nil
	})
}
