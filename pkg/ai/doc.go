// Package ai provides a provider-agnostic SDK for building AI agents in Go.
// It supports text generation, structured object generation, image generation,
// tool calling, streaming, and usage tracking across multiple providers
// (Anthropic, OpenAI, Google).
//
// # Providers
//
// The package uses a registry of [Provider] implementations. Register a provider
// before making any calls:
//
//	import "github.com/sonnes/pi-go/pkg/ai/anthropic"
//
//	p := anthropic.New(anthropic.WithAPIKey("sk-..."))
//	ai.RegisterProvider(p.API(), p)
//
// # Text Generation
//
// Use [GenerateText] for synchronous completions:
//
//	msg, err := ai.GenerateText(
//		ctx,
//		model,
//		ai.Prompt{
//			System:   "You are a helpful assistant.",
//			Messages: []ai.Message{ai.UserMessage("Hello!")},
//		},
//		ai.WithMaxTokens(1024),
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//	for _, c := range msg.Content {
//		if t, ok := ai.AsContent[ai.Text](c); ok {
//			fmt.Println(t.Text)
//		}
//	}
//
// # Streaming
//
// Use [StreamText] for streaming responses. Consume events with Events():
//
//	stream := ai.StreamText(ctx, model, prompt)
//	for event, err := range stream.Events() {
//		if err != nil {
//			log.Fatal(err)
//		}
//		switch event.Type {
//		case ai.EventTextDelta:
//			fmt.Print(event.Delta)
//		case ai.EventDone:
//			fmt.Println()
//		}
//	}
//
// Or use Result() to block until the final message:
//
//	msg, err := ai.StreamText(ctx, model, prompt).Result()
//
// # Tool Calling
//
// Define typed tools with automatic JSON schema generation using [DefineTool]:
//
//	type WeatherInput struct {
//		City string `json:"city"`
//	}
//
//	type WeatherOutput struct {
//		Temp string `json:"temp"`
//	}
//
//	weather := ai.DefineTool[WeatherInput, WeatherOutput](
//		"get_weather",
//		"Get current weather for a city",
//		func(ctx context.Context, in WeatherInput) (WeatherOutput, error) {
//			return WeatherOutput{Temp: "22°C"}, nil
//		},
//	)
//
// Pass tools to a prompt via [Prompt.Tools]:
//
//	msg, err := ai.GenerateText(ctx, model, ai.Prompt{
//		Messages: []ai.Message{ai.UserMessage("Weather in Paris?")},
//		Tools:    []ai.ToolInfo{weather.Info()},
//	})
//
// When the model returns a [ToolCall], execute it and send the result back:
//
//	for _, c := range msg.Content {
//		if tc, ok := ai.AsContent[ai.ToolCall](c); ok {
//			result, err := weather.Run(ctx, ai.ToolCallReq{
//				ID:    tc.ID,
//				Name:  tc.Name,
//				Input: `{"city": "Paris"}`,
//			})
//			// send result back in a ToolResultMessage...
//		}
//	}
//
// # Structured Object Generation
//
// Use [GenerateObject] to generate typed objects:
//
//	type Person struct {
//		Name string `json:"name"`
//		Age  int    `json:"age"`
//	}
//
//	result, err := ai.GenerateObject[Person](ctx, model, ai.Prompt{
//		Messages: []ai.Message{
//			ai.UserMessage("Generate info about Alice, age 30."),
//		},
//	})
//	fmt.Println(result.Object.Name) // "Alice"
//
// # Options
//
// Configure requests with functional [Option] values:
//
//	ai.GenerateText(ctx, model, prompt,
//		ai.WithTemperature(0.7),
//		ai.WithMaxTokens(2048),
//		ai.WithThinking(ai.ThinkingHigh),
//	)
package ai
