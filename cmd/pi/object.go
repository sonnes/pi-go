package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/pi"
)

// objectCommand exercises structured output via [catalog.GenerateObject]. It
// resolves the model spec to a provider, registers it in pi.Default, and asks
// the model to fill a free-form JSON object derived from the prompt.
func objectCommand() *cli.Command {
	return &cli.Command{
		Name:      "object",
		Usage:     "Generate a structured JSON object (ai.GenerateObject)",
		ArgsUsage: "<prompt>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "model",
				Value: "claude-cli/sonnet",
				Usage: "Model spec. A 'claude-cli/', 'codex-cli/', or 'cursor-cli/' prefix selects that stateless CLI provider; anything else is sent verbatim as the model ID with the provider auto-detected (or set via --provider)",
			},
			&cli.StringFlag{
				Name:  "provider",
				Usage: "Provider name (anthropic, openai, google) — overrides auto-detection",
			},
		},
		Action: runObject,
	}
}

func runObject(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() == 0 {
		return fmt.Errorf("usage: pi object [--model spec] <prompt>")
	}
	prompt := strings.Join(cmd.Args().Slice(), " ")

	spec, err := selectAPISpec(cmd.String("model"), cmd.String("provider"))
	if err != nil {
		return err
	}

	result, err := catalog.GenerateObject[map[string]any](
		ctx,
		pi.Default,
		spec,
		ai.Prompt{Messages: []ai.Message{ai.UserMessage(prompt)}},
	)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(result.Object, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	logEvent(colorGreen, "usage", usageFields(result.Usage)...)
	return nil
}
