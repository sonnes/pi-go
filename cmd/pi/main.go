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
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/urfave/cli/v3"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/agent/claude"
	codexagent "github.com/sonnes/pi-go/pkg/agent/codex"
	cursoragent "github.com/sonnes/pi-go/pkg/agent/cursor"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/ai/oauth"
	"github.com/sonnes/pi-go/pkg/ai/provider/anthropic"
	claudeprov "github.com/sonnes/pi-go/pkg/ai/provider/claudecli"
	codexprov "github.com/sonnes/pi-go/pkg/ai/provider/codexcli"
	cursorprov "github.com/sonnes/pi-go/pkg/ai/provider/cursorcli"
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

	// One session-level subscription drains every event, including
	// session_init (first Send) and session_end (Close). Per-Send
	// synchronization uses agent.Wait. Close before wg.Wait so the
	// subscriber channel closes cleanly after session_end.
	ch := a.Subscribe(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for pe := range ch {
			handleEvent(pe.Payload())
		}
	}()
	defer wg.Wait()
	defer a.Close()

	// Single-shot if prompt provided as args.
	if args := cmd.Args(); args.Len() > 0 {
		prompt := strings.Join(args.Slice(), " ")
		return sendAndWait(ctx, a, prompt)
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
		if err := sendAndWait(ctx, a, line); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		fmt.Fprint(os.Stderr, "\n> ")
	}

	return scanner.Err()
}

func createAgent(model string, turns int, tools, serverTools, provider string) (agent.Agent, error) {
	agent.RegisterAgent("claude", claude.New)
	agent.RegisterAgent("codex", codexagent.New)
	agent.RegisterAgent("cursor", cursoragent.New)

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
		return agent.Create(model, opts...)
	default:
		return createAPIAgent(model, turns, serverTools, provider)
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

// claudeCLIModelPrefix selects the stateless claude-cli provider in
// api mode. Example: --model claude-cli/sonnet
const claudeCLIModelPrefix = "claude-cli/"

// codexCLIModelPrefix selects the stateless codex-cli provider in api mode.
// Example: --model codex-cli/gpt-5.4
const codexCLIModelPrefix = "codex-cli/"

// cursorCLIModelPrefix selects the stateless cursor-cli provider in api mode.
// Example: --model cursor-cli/gpt-5
const cursorCLIModelPrefix = "cursor-cli/"

// openAICodexBaseURL is the ChatGPT/Codex Responses API mount. ChatGPT
// OAuth access tokens are honored only on this backend, not on the
// standard api.openai.com Chat Completions endpoint.
const openAICodexBaseURL = "https://chatgpt.com/backend-api/codex"

// newOpenAIOAuthProvider builds an OpenAI Responses provider authenticated
// with a ChatGPT/Codex OAuth token. It routes through the Codex base URL
// because these tokens are rejected (insufficient_quota) on the standard
// Chat Completions endpoint. Optional refresh options persist rotated tokens.
//
// The Codex backend also requires a chatgpt-account-id header identifying the
// account; without it every request fails with a misleading "model not
// supported" 400. If accountID is empty it is read from the access token's
// JWT claims; callers that already have it (e.g. the Codex CLI reuse tier)
// pass it explicitly.
func newOpenAIOAuthProvider(
	clientID string,
	accountID string,
	creds oauth.Credentials,
	refresh ...oauth.TransportOption,
) ai.Provider {
	// Layer the debug transport under OAuth so the verbose log captures the
	// final wire request (post-refresh, post-header-injection).
	opts0 := append(
		[]oauth.TransportOption{oauth.WithBase(maybeDebugTransport(http.DefaultTransport))},
		refresh...,
	)
	transport := openai.NewOAuthTransport(clientID, creds, opts0...)
	opts := []oaioption.RequestOption{
		oaioption.WithBaseURL(openAICodexBaseURL),
		oaioption.WithHTTPClient(&http.Client{Transport: transport}),
	}
	if accountID == "" {
		accountID = chatgptAccountID(creds.AccessToken)
	}
	if accountID != "" {
		opts = append(opts, oaioption.WithHeader("chatgpt-account-id", accountID))
	}
	// Codex backend requires its dialect: it enforces a non-empty
	// `instructions` field that the default OpenAI Responses dialect omits
	// when the caller has no system prompt.
	return openairesponses.NewForCodex(opts...)
}

// chatgptAccountID extracts the ChatGPT account ID from an OpenAI OAuth
// access token. The token is a JWT whose payload carries the ID under the
// "https://api.openai.com/auth" claim. It returns "" if token is not a
// well-formed JWT or the claim is absent — the account ID is stable across
// refreshes, so decoding the initial token once is sufficient.
func chatgptAccountID(token string) string {
	payload, err := jwtPayload(token)
	if err != nil {
		return ""
	}

	var claims struct {
		Auth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Auth.ChatGPTAccountID
}

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
			return newOpenAIOAuthProvider(clientID, "", creds), nil
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

// selectProvider resolves a model spec to a provider and the bare model ID.
// A "claude-cli/", "codex-cli/", or "cursor-cli/" prefix picks the matching
// stateless CLI provider; anything else is auto-detected from credentials
// (or forced via providerHint), and the spec is used verbatim as the model ID.
func selectProvider(model, providerHint string) (ai.Provider, string, error) {
	if rest, ok := strings.CutPrefix(model, claudeCLIModelPrefix); ok {
		fmt.Fprintln(os.Stderr, "[provider: claude-cli via subprocess]")
		return claudeprov.New(claudeprov.WithModel(rest)), rest, nil
	}
	if rest, ok := strings.CutPrefix(model, codexCLIModelPrefix); ok {
		fmt.Fprintln(os.Stderr, "[provider: codex-cli via subprocess]")
		return codexprov.New(codexprov.WithModel(rest)), rest, nil
	}
	if rest, ok := strings.CutPrefix(model, cursorCLIModelPrefix); ok {
		fmt.Fprintln(os.Stderr, "[provider: cursor-cli via subprocess]")
		return cursorprov.New(cursorprov.WithModel(rest)), rest, nil
	}

	p, _, err := detectProvider(providerHint)
	if err != nil {
		return nil, "", err
	}
	return p, model, nil
}

func createAPIAgent(model string, turns int, serverToolsSpec, providerHint string) (agent.Agent, error) {
	p, modelID, err := selectProvider(model, providerHint)
	if err != nil {
		return nil, err
	}

	ai.RegisterProvider(p.Provider(), p)

	m := ai.Model{
		ID:       modelID,
		Name:     modelID,
		Provider: p.Provider(),
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

	return agent.New(m, opts...), nil
}

// isAnthropicOAuthToken reports whether token is an Anthropic OAuth
// token based on the "sk-ant-oat" prefix convention.
func isAnthropicOAuthToken(token string) bool {
	return strings.Contains(token, "sk-ant-oat")
}

func detectProvider(hint string) (ai.Provider, string, error) {
	// Precedence: explicit `pi login` credentials in ~/.pigo/auth.json, then
	// subscription logins reused from official provider CLIs (Claude Code,
	// Codex), then API keys / OAuth tokens from environment variables.
	if p, name, err := detectFromAuthFile(hint); err == nil {
		return p, name, nil
	}

	if p, name, err := detectFromCLICreds(hint); err == nil {
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
				anthropic.WithOAuth(
					sc.ClientID, creds,
					oauth.WithBase(maybeDebugTransport(http.DefaultTransport)),
					persistRefresh(sc),
				),
			), nil
		},
	},
	{
		name: "openai",
		create: func(sc StoredCredential) (ai.Provider, error) {
			creds := sc.ToOAuthCredentials()
			return newOpenAIOAuthProvider(
				sc.ClientID, "", creds, persistRefresh(sc),
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

func sendAndWait(ctx context.Context, a agent.Agent, prompt string) error {
	if err := a.Send(ctx, prompt); err != nil {
		return err
	}
	_, err := a.Wait(ctx)
	return err
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

func errField(err error) string {
	if err == nil {
		return ""
	}
	return "err=" + err.Error()
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
	case agent.EventSessionInit:
		logEvent(colorCyan+colorBold, "session:init", optField("sid", evt.SessionID))

	case agent.EventSessionEnd:
		logEvent(colorCyan+colorBold, "session:end", errField(evt.Err))

	case agent.EventAgentStart:
		logEvent(colorBlue, "agent:start", optField("sid", evt.SessionID))

	case agent.EventAgentEnd:
		// Insert a blank line so streamed assistant text on stdout
		// doesn't visually run into the trailing meta block.
		fmt.Fprintln(os.Stderr)
		if evt.Usage.Total > 0 {
			logEvent(colorGreen, "usage", usageFields(evt.Usage)...)
		}
		if evt.Err != nil {
			logEvent(colorRed+colorBold, "error", "msg="+evt.Err.Error())
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
