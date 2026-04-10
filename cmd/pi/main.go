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
	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
	claudeprov "github.com/sonnes/pi-go/pkg/ai/provider/claude"
	"github.com/sonnes/pi-go/pkg/ai/provider/google"
	"github.com/sonnes/pi-go/pkg/ai/provider/openai"

	oaioption "github.com/openai/openai-go/option"
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
		return createAPIAgent(model, turns)
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

// claudeCLIModelPrefix selects the stateless claude-cli provider in
// api mode. Example: --agent api --model claude-cli/sonnet
const claudeCLIModelPrefix = "claude-cli/"

// providerEntry describes how to detect and create an AI provider from
// an environment variable. Entries are checked in order; the first
// match wins.
type providerEntry struct {
	envKey string
	name   string
	create func(apiKey string) (ai.Provider, error)
}

var providers = []providerEntry{
	{
		envKey: "ANTHROPIC_API_KEY",
		name:   "Anthropic",
		create: func(apiKey string) (ai.Provider, error) {
			// ANTHROPIC_OAUTH_TOKEN takes priority over ANTHROPIC_API_KEY.
			if token := os.Getenv("ANTHROPIC_OAUTH_TOKEN"); token != "" {
				apiKey = token
			}
			if isAnthropicOAuthToken(apiKey) {
				creds := oauth.Credentials{AccessToken: apiKey}
				return anthropic.New(anthropic.WithOAuth(creds)), nil
			}
			return anthropic.New(anthropic.WithAPIKey(apiKey)), nil
		},
	},
	{
		envKey: "OPENROUTER_API_KEY",
		name:   "OpenRouter",
		create: func(apiKey string) (ai.Provider, error) {
			return openai.New(
				oaioption.WithAPIKey(apiKey),
				oaioption.WithBaseURL("https://openrouter.ai/api/v1"),
			), nil
		},
	},
	{
		envKey: "OPENAI_API_KEY",
		name:   "OpenAI",
		create: func(apiKey string) (ai.Provider, error) {
			return openai.New(oaioption.WithAPIKey(apiKey)), nil
		},
	},
	{
		envKey: "GOOGLE_API_KEY",
		name:   "Google",
		create: func(apiKey string) (ai.Provider, error) {
			return google.New(google.WithAPIKey(apiKey))
		},
	},
}

func createAPIAgent(model string, turns int) (agent.Agent, error) {
	var (
		p    ai.Provider
		name string
	)

	if rest, ok := strings.CutPrefix(model, claudeCLIModelPrefix); ok {
		model = rest
		p = claudeprov.New(claudeprov.WithModel(model))
		name = "claude-cli"
		fmt.Fprintln(os.Stderr, "[provider: claude-cli via subprocess]")
	} else {
		detected, detectedName, err := detectProvider()
		if err != nil {
			return nil, err
		}
		p = detected
		name = detectedName
	}

	ai.RegisterProvider(p.API(), p)

	m := ai.Model{
		ID:       model,
		Name:     model,
		API:      p.API(),
		Provider: name,
	}

	var opts []agent.Option
	if turns > 0 {
		opts = append(opts, agent.WithMaxTurns(turns))
	}

	return agent.New(m, opts...), nil
}

// isAnthropicOAuthToken reports whether token is an Anthropic OAuth
// token based on the "sk-ant-oat" prefix convention.
func isAnthropicOAuthToken(token string) bool {
	return strings.Contains(token, "sk-ant-oat")
}

func detectProvider() (ai.Provider, string, error) {
	for _, pe := range providers {
		apiKey := os.Getenv(pe.envKey)
		if apiKey == "" {
			continue
		}

		p, err := pe.create(apiKey)
		if err != nil {
			return nil, "", fmt.Errorf("init %s provider: %w", pe.name, err)
		}

		fmt.Fprintf(os.Stderr, "[provider: %s via %s]\n", pe.name, pe.envKey)
		return p, pe.name, nil
	}

	return nil, "", fmt.Errorf(
		"no API key found; set one of: ANTHROPIC_API_KEY, ANTHROPIC_OAUTH_TOKEN, OPENROUTER_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY",
	)
}

func sendAndPrint(ctx context.Context, a agent.Agent, prompt string) error {
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := a.Subscribe(subCtx)

	if err := a.Send(ctx, prompt); err != nil {
		return err
	}

	for pe := range ch {
		evt := pe.Payload()
		handleEvent(evt)
		if evt.Type == agent.EventAgentEnd {
			return evt.Err
		}
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
