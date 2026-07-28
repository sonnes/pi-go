package demo

import (
	"context"
	"errors"
	"fmt"

	"github.com/openai/openai-go/option"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"
)

// DefaultLiveModel is a free, tool-calling model on OpenRouter, so a
// visitor who connects their account pays nothing to try the demo.
// The page can override it per command.
const DefaultLiveModel = "inclusionai/ling-3.0-flash:free"

// baseURL is required: NewForOpenRouter selects the dialect but leaves
// the endpoint to the caller, so without this the SDK's default sends
// the request to api.openai.com with an OpenRouter key.
const baseURL = "https://openrouter.ai/api/v1"

// referer identifies this app to OpenRouter for attribution. It is not
// a credential.
const (
	referer = "https://sonnes.github.io/pi-go/"
	appName = "pi-go demo"
)

// useKey switches the demo to live mode against OpenRouter using a key
// the visitor supplied — from the PKCE flow or pasted by hand. The key
// never leaves the browser: it goes straight into the provider that
// runs inside this wasm module.
//
// Switching starts a fresh session rather than continuing the scripted
// one, so a transcript is never half canned and half real.
func (d *Demo) useKey(ctx context.Context, key, model string) error {
	if key == "" {
		return errors.New("demo: live mode needs an OpenRouter key")
	}

	if model == "" {
		model = DefaultLiveModel
	}

	provider := openairesponses.NewForOpenRouter(
		option.WithAPIKey(key),
		option.WithBaseURL(baseURL),
		option.WithHeader("HTTP-Referer", referer),
		option.WithHeader("X-Title", appName),
	)

	if err := d.Close(); err != nil {
		return err
	}

	d.mode = ModeLive
	d.model = model
	d.lm = ai.NewLanguageModel(ai.Model{ID: model}, provider)

	if err := d.open(ctx); err != nil {
		return fmt.Errorf("demo: live mode: %w", err)
	}

	d.emit(Event{Kind: KindStatus, Text: "live on " + model, Mode: ModeLive, Model: model})

	return nil
}
