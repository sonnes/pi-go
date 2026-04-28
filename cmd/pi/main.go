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
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/agent/claude"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
	claudeprov "github.com/sonnes/pi-go/pkg/ai/provider/claudecli"
	"github.com/sonnes/pi-go/pkg/ai/provider/geminicli"
	"github.com/sonnes/pi-go/pkg/ai/provider/google"
	"github.com/sonnes/pi-go/pkg/ai/provider/openai"
	"github.com/sonnes/pi-go/pkg/ai/provider/openairesponses"

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
			&cli.StringFlag{
				Name:  "server-tools",
				Usage: "Provider-hosted server tools for api mode (comma-separated: web_search, code_execution)",
			},
			&cli.StringFlag{
				Name:  "provider",
				Usage: "Provider name (anthropic, openai, google) — overrides auto-detection",
			},
		},
		Action: run,
		Commands: []*cli.Command{
			loginCommand(),
			logoutCommand(),
		},
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
	serverTools := cmd.String("server-tools")
	provider := cmd.String("provider")

	a, err := createAgent(mode, model, turns, tools, serverTools, provider)
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

func createAgent(mode, model string, turns int, tools, serverTools, provider string) (agent.Agent, error) {
	switch mode {
	case "claude":
		return createClaudeAgent(model, turns, tools), nil
	case "api":
		return createAPIAgent(model, turns, serverTools, provider)
	default:
		return nil, fmt.Errorf("unknown agent mode: %s (use claude or api)", mode)
	}
}

// parseServerTools converts a comma-separated list of server-tool names
// (e.g. "web_search,code_execution") into [ai.Tool] entries built via
// [ai.DefineServerTool]. Empty input returns nil.
func parseServerTools(spec string) ([]ai.Tool, error) {
	if spec == "" {
		return nil, nil
	}

	known := map[string]ai.ServerToolType{
		"web_search":     ai.ServerToolWebSearch,
		"code_execution": ai.ServerToolCodeExecution,
		"web_fetch":      ai.ServerToolWebFetch,
		"file_search":    ai.ServerToolFileSearch,
		"computer":       ai.ServerToolComputer,
		"bash":           ai.ServerToolBash,
		"text_editor":    ai.ServerToolTextEditor,
		"tool_search":    ai.ServerToolToolSearch,
		"mcp":            ai.ServerToolMCP,
	}

	var tools []ai.Tool
	for _, raw := range strings.Split(spec, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		typ, ok := known[name]
		if !ok {
			return nil, fmt.Errorf("unknown server tool %q (known: web_search, code_execution, web_fetch, file_search, computer, bash, text_editor, tool_search, mcp)", name)
		}
		tools = append(tools, ai.DefineServerTool(ai.ToolInfo{ServerType: typ}))
	}
	return tools, nil
}

func createClaudeAgent(model string, turns int, tools string) agent.Agent {
	opts := []agent.Option{
		agent.WithModelName(model),
	}
	if turns > 0 {
		opts = append(opts, agent.WithMaxTurns(turns))
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
				clientID := os.Getenv("ANTHROPIC_OAUTH_CLIENT_ID")
				creds := oauth.Credentials{AccessToken: apiKey}
				return anthropic.New(anthropic.WithOAuth(clientID, creds)), nil
			}
			return anthropic.New(anthropic.WithAPIKey(apiKey)), nil
		},
	},
	{
		envKey: "OPENROUTER_API_KEY",
		name:   "OpenRouter",
		create: func(apiKey string) (ai.Provider, error) {
			return openairesponses.NewForOpenRouter(
				oaioption.WithAPIKey(apiKey),
				oaioption.WithBaseURL("https://openrouter.ai/api/v1"),
			), nil
		},
	},
	{
		envKey: "OPENAI_OAUTH_TOKEN",
		name:   "OpenAI",
		create: func(token string) (ai.Provider, error) {
			clientID := os.Getenv("OPENAI_OAUTH_CLIENT_ID")
			creds := oauth.Credentials{AccessToken: token}
			return openai.NewWithOAuth(clientID, creds), nil
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
		envKey: "GOOGLE_OAUTH_TOKEN",
		name:   "Gemini CLI",
		create: func(token string) (ai.Provider, error) {
			clientID := os.Getenv("GOOGLE_OAUTH_CLIENT_ID")
			clientSecret := os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")
			creds := oauth.Credentials{AccessToken: token}
			return geminicli.New(
				geminicli.WithOAuth(clientID, clientSecret, creds),
			), nil
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

func createAPIAgent(model string, turns int, serverToolsSpec, providerHint string) (agent.Agent, error) {
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
		detected, detectedName, err := detectProvider(providerHint)
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

	opts := []agent.Option{agent.WithModel(m)}
	if turns > 0 {
		opts = append(opts, agent.WithMaxTurns(turns))
	}

	serverTools, err := parseServerTools(serverToolsSpec)
	if err != nil {
		return nil, err
	}
	if len(serverTools) > 0 {
		opts = append(opts, agent.WithTools(serverTools...))
		fmt.Fprintf(os.Stderr, "[server tools: %s]\n", serverToolsSpec)
	}

	return agent.New(opts...), nil
}

// isAnthropicOAuthToken reports whether token is an Anthropic OAuth
// token based on the "sk-ant-oat" prefix convention.
func isAnthropicOAuthToken(token string) bool {
	return strings.Contains(token, "sk-ant-oat")
}

func detectProvider(hint string) (ai.Provider, string, error) {
	// Try stored OAuth credentials from ~/.pigo/auth.json first.
	if p, name, err := detectFromAuthFile(hint); err == nil {
		return p, name, nil
	}

	// Fall back to environment variables.
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
		"no API key found; set one of: ANTHROPIC_API_KEY, ANTHROPIC_OAUTH_TOKEN, OPENROUTER_API_KEY, OPENAI_API_KEY, OPENAI_OAUTH_TOKEN, GOOGLE_API_KEY, GOOGLE_OAUTH_TOKEN",
	)
}

// authProviderOrder defines the priority when loading from auth.json.
var authProviderOrder = []struct {
	name   string
	create func(sc StoredCredential) (ai.Provider, error)
}{
	{
		name: "anthropic",
		create: func(sc StoredCredential) (ai.Provider, error) {
			creds := sc.ToOAuthCredentials()
			return anthropic.New(
				anthropic.WithOAuth(sc.ClientID, creds, persistRefresh(sc)),
			), nil
		},
	},
	{
		name: "openai",
		create: func(sc StoredCredential) (ai.Provider, error) {
			creds := sc.ToOAuthCredentials()
			return openai.NewWithOAuth(
				sc.ClientID, creds, persistRefresh(sc),
			), nil
		},
	},
	{
		name: "google",
		create: func(sc StoredCredential) (ai.Provider, error) {
			creds := sc.ToOAuthCredentials()
			return geminicli.New(
				geminicli.WithOAuth(
					sc.ClientID, sc.ClientSecret, creds, persistRefresh(sc),
				),
			), nil
		},
	},
}

// detectFromAuthFile tries to create a provider from stored OAuth credentials.
// If hint is non-empty, only that provider is tried.
func detectFromAuthFile(hint string) (ai.Provider, string, error) {
	stored, err := LoadAuth()
	if err != nil || len(stored) == 0 {
		return nil, "", fmt.Errorf("no stored credentials")
	}

	for _, entry := range authProviderOrder {
		if hint != "" && entry.name != hint {
			continue
		}
		sc, ok := stored[entry.name]
		if !ok {
			continue
		}

		p, err := entry.create(sc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[auth.json: %s failed: %v]\n", entry.name, err)
			continue
		}

		fmt.Fprintf(os.Stderr, "[provider: %s via ~/.pigo/auth.json]\n", entry.name)
		return p, entry.name, nil
	}

	return nil, "", fmt.Errorf("no usable stored credentials")
}

// persistRefresh returns an [oauth.TransportOption] that writes refreshed
// tokens back to auth.json.
func persistRefresh(sc StoredCredential) oauth.TransportOption {
	return oauth.WithOnRefresh(func(creds oauth.Credentials) error {
		stored, err := LoadAuth()
		if err != nil {
			return err
		}
		stored[findProviderName(sc.ClientID)] = FromOAuthCredentials(
			creds, sc.ClientID, sc.ClientSecret,
		)
		return SaveAuth(stored)
	})
}

// findProviderName returns the provider name for a given client ID
// by scanning the auth file.
func findProviderName(clientID string) string {
	stored, err := LoadAuth()
	if err != nil {
		return ""
	}
	for name, sc := range stored {
		if sc.ClientID == clientID {
			return name
		}
	}
	return ""
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

// printServerToolCall renders a one-line summary of a provider-executed
// server-tool call to stderr — name, arguments, and a truncated rendering
// of its Output (when present).
func printServerToolCall(tc ai.ToolCall) {
	name := string(tc.ServerType)
	if name == "" {
		name = tc.Name
	}
	fmt.Fprintf(os.Stderr, "\n[server tool: %s", name)
	if len(tc.Arguments) > 0 {
		if data, err := json.Marshal(tc.Arguments); err == nil {
			fmt.Fprintf(os.Stderr, " args=%s", data)
		}
	}
	fmt.Fprintln(os.Stderr, "]")
	if tc.Output != nil {
		out := tc.Output.Content
		if len(out) > 200 {
			out = out[:200] + "..."
		}
		marker := "result"
		if tc.Output.IsError {
			marker = "error"
		}
		fmt.Fprintf(os.Stderr, "[%s: %s]\n", marker, out)
	}
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

	case agent.EventMessageEnd:
		// Surface provider-executed server-tool calls that bypassed
		// EventToolExecution* (those events fire only for client-side
		// function tools).
		if evt.Message != nil && evt.Message.Role == ai.RoleAssistant {
			for _, c := range evt.Message.Content {
				tc, ok := ai.AsContent[ai.ToolCall](c)
				if !ok || !tc.Server {
					continue
				}
				printServerToolCall(tc)
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
			if evt.Usage.CacheRead > 0 || evt.Usage.CacheWrite > 0 {
				fmt.Fprintf(
					os.Stderr,
					", %d cache_read, %d cache_write",
					evt.Usage.CacheRead,
					evt.Usage.CacheWrite,
				)
			}
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
