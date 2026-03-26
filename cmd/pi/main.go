// Command pi is a test-drive CLI for the pi-go agent SDK.
//
// Usage:
//
//	pi [flags] [prompt]
//
// If no prompt is given, it reads from stdin line by line for
// interactive multi-turn conversation.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/agent/claude"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
)

func main() {
	cmd := &cli.Command{
		Name:  "pi",
		Usage: "Test-drive the pi-go agent SDK",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "agent",
				Value: "claude",
				Usage: "Agent mode: claude or api",
			},
			&cli.StringFlag{
				Name:  "model",
				Value: "claude-sonnet-4-20250514",
				Usage: "Model ID",
			},
			&cli.IntFlag{
				Name:  "turns",
				Value: 0,
				Usage: "Max agentic turns (0 = unlimited)",
			},
			&cli.StringFlag{
				Name:  "tools",
				Usage: "Allowed tools for claude mode (comma-separated)",
			},
		},
		Action: run,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	mode := cmd.String("agent")
	model := cmd.String("model")
	turns := int(cmd.Int("turns"))
	tools := cmd.String("tools")

	a, err := createAgent(mode, model, turns, tools)
	if err != nil {
		return err
	}

	// Single-shot if prompt provided as args.
	if args := cmd.Args(); args.Len() > 0 {
		prompt := strings.Join(args.Slice(), " ")
		return sendAndPrint(ctx, a, prompt)
	}

	// Interactive multi-turn.
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprint(os.Stderr, "> ")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Fprint(os.Stderr, "> ")
			continue
		}
		if err := sendAndPrint(ctx, a, line); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		fmt.Fprint(os.Stderr, "\n> ")
	}

	return scanner.Err()
}

func createAgent(mode, model string, turns int, tools string) (agent.Agent, error) {
	switch mode {
	case "claude":
		return createClaudeAgent(model, turns, tools), nil
	case "api":
		return createAPIAgent(model, turns), nil
	default:
		return nil, fmt.Errorf("unknown agent mode: %s (use claude or api)", mode)
	}
}

func createClaudeAgent(model string, turns int, tools string) agent.Agent {
	opts := []claude.Option{
		claude.WithModel(model),
	}
	if turns > 0 {
		opts = append(opts, claude.WithMaxTurns(turns))
	}
	if tools != "" {
		opts = append(opts, claude.WithAllowedTools(strings.Split(tools, ",")...))
	}
	return claude.New(opts...)
}

func createAPIAgent(model string, turns int) agent.Agent {
	p := anthropic.New()
	ai.RegisterProvider(p.API(), p)

	m := ai.Model{
		ID:       model,
		Name:     model,
		API:      p.API(),
		Provider: "Anthropic",
	}

	var opts []agent.Option
	if turns > 0 {
		opts = append(opts, agent.WithMaxTurns(turns))
	}

	return agent.New(m, opts...)
}

func sendAndPrint(ctx context.Context, a agent.Agent, prompt string) error {
	stream := a.Send(ctx, prompt)

	for evt, err := range stream.Events(ctx) {
		if err != nil {
			return err
		}
		handleEvent(evt)
	}

	return nil
}

func handleEvent(evt agent.Event) {
	switch evt.Type {
	case agent.EventMessageUpdate:
		if evt.AssistantEvent != nil {
			fmt.Print(evt.AssistantEvent.Delta)
		}

	case agent.EventMessageStart:
		if evt.Message != nil && evt.Message.Role == ai.RoleAssistant {
			// For non-streaming agents (claude subprocess), the full
			// text arrives at message_start. Print it.
			if text := evt.Message.Text(); text != "" {
				fmt.Print(text)
			}
		}

	case agent.EventToolExecutionStart:
		fmt.Fprintf(os.Stderr, "\n[tool: %s]\n", evt.ToolName)

	case agent.EventToolExecutionEnd:
		result := fmt.Sprintf("%v", evt.Result)
		if len(result) > 200 {
			result = result[:200] + "..."
		}
		fmt.Fprintf(os.Stderr, "[result: %s]\n", result)

	case agent.EventAgentEnd:
		fmt.Fprintln(os.Stderr)
		if evt.Usage.Total > 0 {
			fmt.Fprintf(
				os.Stderr,
				"[usage: %d in, %d out, %d total",
				evt.Usage.Input,
				evt.Usage.Output,
				evt.Usage.Total,
			)
			if evt.Usage.Cost.Total > 0 {
				fmt.Fprintf(os.Stderr, ", $%.4f", evt.Usage.Cost.Total)
			}
			fmt.Fprintln(os.Stderr, "]")
		}
		if evt.Err != nil {
			fmt.Fprintf(os.Stderr, "[error: %v]\n", evt.Err)
		}
	}
}
