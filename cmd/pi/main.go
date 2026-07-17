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
	codexagent "github.com/sonnes/pi-go/pkg/agent/codex"
	cursoragent "github.com/sonnes/pi-go/pkg/agent/cursor"
	"github.com/sonnes/pi-go/pkg/ai"
	claudeprov "github.com/sonnes/pi-go/pkg/ai/provider/claudecli"
	codexprov "github.com/sonnes/pi-go/pkg/ai/provider/codexcli"
	cursorprov "github.com/sonnes/pi-go/pkg/ai/provider/cursorcli"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/pi"
)

// init wires the CLI-subprocess agents so pi.Default routes "claude/…",
// "codex/…", and "cursor/…" specs to them, and registers the pi-CLI
// credential detectors (stored `pi login` credentials and reused
// official-CLI logins) ahead of pkg/pi's built-in environment detectors.
func init() {
	pi.Default.RegisterAgent("claude", claude.Factory())
	pi.Default.RegisterAgent("codex", codexagent.Factory())
	pi.Default.RegisterAgent("cursor", cursoragent.Factory())
	pi.AddDetector(loginDetectors()...)
}

func main() {
	cmd := &cli.Command{
		Name:  "pi",
		Usage: "Test-drive the pi-go agent SDK",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "model",
				Value: "claude/sonnet",
				Usage: "Model spec. A 'claude/', 'codex/', or 'cursor/' prefix routes to that CLI subprocess agent; 'claude-cli/', 'codex-cli/', 'cursor-cli/' select the matching stateless CLI provider in api mode; anything else is sent verbatim as the model ID in api mode, with the provider auto-detected (or set via --provider)",
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
			objectCommand(),
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	model := cmd.String("model")
	turns := int(cmd.Int("turns"))
	tools := cmd.String("tools")
	serverTools := cmd.String("server-tools")
	provider := cmd.String("provider")

	a, err := createAgent(model, turns, tools, serverTools, provider)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := a.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "close error: %v\n", cerr)
		}
	}()

	// Single-shot if prompt provided as args.
	if args := cmd.Args(); args.Len() > 0 {
		prompt := strings.Join(args.Slice(), " ")
		return runPrompt(ctx, a, prompt)
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
		if err := runPrompt(ctx, a, line); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		fmt.Fprint(os.Stderr, "\n> ")
	}

	return scanner.Err()
}

func createAgent(model string, turns int, tools, serverTools, provider string) (agent.Agent, error) {
	kind, _, _ := strings.Cut(model, "/")
	switch kind {
	case "claude", "codex", "cursor":
		opts := []agent.Option{}
		if turns > 0 {
			opts = append(opts, agent.WithMaxTurns(turns))
		}
		if kind == "claude" && tools != "" {
			opts = append(opts, claude.WithAllowedTools(strings.Split(tools, ",")...))
		}
		return pi.Default.Agent(model, opts...)
	default:
		return createAPIAgent(model, turns, serverTools, provider)
	}
}

func createAPIAgent(model string, turns int, serverToolsSpec, providerHint string) (agent.Agent, error) {
	spec, err := selectAPISpec(model, providerHint)
	if err != nil {
		return nil, err
	}

	var opts []agent.Option
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

	return pi.Default.Agent(spec, opts...)
}

// selectAPISpec resolves an api-mode model to a "<provider>/<model>" catalog
// spec, registering the provider and the requested model in pi.Default so the
// spec resolves. A "claude-cli/", "codex-cli/", or "cursor-cli/" prefix picks
// the matching stateless subprocess provider; anything else auto-detects
// credentials via pkg/pi (honoring providerHint), with the spec used verbatim
// as the model ID.
func selectAPISpec(model, providerHint string) (string, error) {
	register := func(p catalog.Provider, id string) string {
		pi.Default.RegisterProvider(p)
		pi.Default.RegisterModel(p.ID(), ai.Model{ID: id, Name: id})
		return p.ID() + "/" + id
	}

	cliProviders := []struct {
		prefix string
		label  string
		build  func(model string) catalog.Provider
	}{
		{claudeCLIModelPrefix, "claude-cli", func(m string) catalog.Provider { return claudeprov.New(claudeprov.WithModel(m)) }},
		{codexCLIModelPrefix, "codex-cli", func(m string) catalog.Provider { return codexprov.New(codexprov.WithModel(m)) }},
		{cursorCLIModelPrefix, "cursor-cli", func(m string) catalog.Provider { return cursorprov.New(cursorprov.WithModel(m)) }},
	}
	for _, e := range cliProviders {
		if rest, ok := strings.CutPrefix(model, e.prefix); ok {
			fmt.Fprintf(os.Stderr, "[provider: %s via subprocess]\n", e.label)
			return register(e.build(rest), rest), nil
		}
	}

	det, err := pi.Detect(providerHint)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "[provider: %s via %s]\n", det.Name, det.Source)
	pi.Default.RegisterModel(det.Provider, ai.Model{ID: model, Name: model})
	return det.Provider + "/" + model, nil
}

// claudeCLIModelPrefix selects the stateless claude-cli provider in
// api mode. Example: --model claude-cli/sonnet
const claudeCLIModelPrefix = "claude-cli/"

// codexCLIModelPrefix selects the stateless codex-cli provider in api mode.
// Example: --model codex-cli/gpt-5.4
const codexCLIModelPrefix = "codex-cli/"

// cursorCLIModelPrefix selects the stateless cursor-cli provider in api mode.
// Example: --model cursor-cli/gpt-5
const cursorCLIModelPrefix = "cursor-cli/"

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

// runPrompt sends one prompt and streams the run's events until it ends.
func runPrompt(ctx context.Context, a agent.Agent, prompt string) error {
	s := a.Run(ctx, ai.UserMessage(prompt))
	for evt, err := range s.Events() {
		if err != nil {
			return err
		}
		handleEvent(evt)
	}
	return nil
}

// ANSI colors for stderr event log. Always emitted — modern terminals
// handle them, and they remain readable when stderr is redirected to a
// file or piped through `less -R`.
const (
	colorReset  = "\033[0m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
)

// logEvent writes one bracketed, colorized event line to stderr. Empty
// fields are skipped so callers can pass conditional values without
// pre-filtering.
func logEvent(color, label string, fields ...string) {
	var b strings.Builder
	b.WriteString(color)
	b.WriteByte('[')
	b.WriteString(label)
	b.WriteByte(']')
	b.WriteString(colorReset)
	for _, f := range fields {
		if f == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(f)
	}
	fmt.Fprintln(os.Stderr, b.String())
}

func optField(key, val string) string {
	if val == "" {
		return ""
	}
	return key + "=" + val
}

func usageFields(u ai.Usage) []string {
	fields := []string{
		fmt.Sprintf("in=%d", u.Input),
		fmt.Sprintf("out=%d", u.Output),
		fmt.Sprintf("total=%d", u.Total),
	}
	if u.CacheRead > 0 {
		fields = append(fields, fmt.Sprintf("cache_read=%d", u.CacheRead))
	}
	if u.CacheWrite > 0 {
		fields = append(fields, fmt.Sprintf("cache_write=%d", u.CacheWrite))
	}
	if u.Cost.Total > 0 {
		fields = append(fields, fmt.Sprintf("$%.4f", u.Cost.Total))
	}
	return fields
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// printServerToolCall renders a provider-executed server-tool call to
// stderr — name, arguments, and a truncated rendering of its Output
// (when present).
func printServerToolCall(tc ai.ToolCall) {
	name := string(tc.ServerType)
	if name == "" {
		name = tc.Name
	}
	var argField string
	if len(tc.Arguments) > 0 {
		if data, err := json.Marshal(tc.Arguments); err == nil {
			argField = "args=" + truncate(string(data), 200)
		}
	}
	logEvent(colorYellow, "server-tool:"+name, argField)
	if tc.Output == nil {
		return
	}
	out := truncate(tc.Output.Content, 200)
	if tc.Output.IsError {
		logEvent(colorRed, "server-tool:"+name+":error", "result="+out)
	} else {
		logEvent(colorGreen, "server-tool:"+name+":done", "result="+out)
	}
}

func handleEvent(evt agent.Event) {
	switch evt.Type {
	case agent.EventAgentStart:
		logEvent(colorBlue, "agent:start", optField("sid", evt.SessionID))

	case agent.EventAgentEnd:
		// Insert a blank line so streamed assistant text on stdout
		// doesn't visually run into the trailing meta block.
		fmt.Fprintln(os.Stderr)
		if evt.Usage.Total > 0 {
			logEvent(colorGreen, "usage", usageFields(evt.Usage)...)
		}
		logEvent(colorBlue, "agent:end")

	case agent.EventTurnStart:
		logEvent(colorDim, "turn:start")

	case agent.EventTurnEnd:
		// Streamed assistant text on stdout doesn't end in \n; insert
		// one on stderr so the bracketed event lands on its own line.
		fmt.Fprintln(os.Stderr)
		logEvent(colorDim, "turn:end")

	case agent.EventMessageStart:
		if evt.Message != nil && evt.Message.Role == ai.RoleAssistant {
			// For non-streaming agents (claude subprocess), the full
			// text arrives at message_start. Print it.
			if text := evt.Message.Text(); text != "" {
				fmt.Print(text)
			}
		}

	case agent.EventMessageUpdate:
		if evt.AssistantEvent != nil {
			fmt.Print(evt.AssistantEvent.Delta)
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
		var argField string
		if len(evt.Args) > 0 {
			if data, err := json.Marshal(evt.Args); err == nil {
				argField = "args=" + truncate(string(data), 200)
			}
		}
		logEvent(colorYellow, "tool:"+evt.ToolName, argField)

	case agent.EventToolExecutionUpdate:
		if evt.PartialResult != nil {
			partial := truncate(fmt.Sprintf("%v", evt.PartialResult), 100)
			logEvent(colorYellow+colorDim, "tool:"+evt.ToolName+":update", "partial="+partial)
		}

	case agent.EventToolExecutionEnd:
		result := truncate(fmt.Sprintf("%v", evt.Result), 200)
		if evt.IsError {
			logEvent(colorRed, "tool:"+evt.ToolName+":error", "result="+result)
		} else {
			logEvent(colorGreen, "tool:"+evt.ToolName+":done", "result="+result)
		}
	}
}
