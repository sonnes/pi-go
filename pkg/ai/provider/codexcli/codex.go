// Package codexcli provides an [ai.Provider] and [ai.ObjectProvider]
// implementation backed by the `codex` CLI running in non-interactive
// `exec --json` mode.
package codexcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/sonnes/pi-go/pkg/ai"
)

var (
	_ ai.Provider       = (*Provider)(nil)
	_ ai.ObjectProvider = (*Provider)(nil)
)

// Provider implements [ai.Provider] and [ai.ObjectProvider] by delegating
// each call to a fresh `codex exec --json` subprocess.
type Provider struct {
	cfg config

	// sendFn spawns the subprocess. Defaults to [spawn]; overridden in tests.
	sendFn func(ctx context.Context, cfg config, args sendArgs) (io.ReadCloser, func() error, error)
}

// config holds all configuration for the provider.
type config struct {
	cliPath          string
	workDir          string
	addDirs          []string
	env              []string
	model            string
	sandbox          string
	approvalPolicy   string
	skipGitRepoCheck bool
	ignoreUserConfig bool
	ignoreRules      bool
}

// Option configures a [Provider].
type Option func(*config)

// WithCLIPath sets the path to the codex CLI binary. Defaults to "codex".
func WithCLIPath(path string) Option {
	return func(c *config) { c.cliPath = path }
}

// WithWorkDir sets the working root for each subprocess.
func WithWorkDir(dir string) Option {
	return func(c *config) { c.workDir = dir }
}

// WithAddDirs adds additional writable directories via --add-dir flags.
func WithAddDirs(dirs ...string) Option {
	return func(c *config) { c.addDirs = dirs }
}

// WithEnv sets additional environment variables for each subprocess.
// Each entry should be in "KEY=VALUE" format.
func WithEnv(env ...string) Option {
	return func(c *config) { c.env = env }
}

// WithModel overrides the default model. Per-call [ai.Model.ID] values
// take precedence over this setting.
func WithModel(model string) Option {
	return func(c *config) { c.model = model }
}

// WithSandbox sets the Codex sandbox mode, such as "read-only",
// "workspace-write", or "danger-full-access".
func WithSandbox(mode string) Option {
	return func(c *config) { c.sandbox = mode }
}

// WithApprovalPolicy sets the Codex approval policy. Defaults to "never"
// so non-interactive calls cannot hang waiting for terminal approval.
func WithApprovalPolicy(policy string) Option {
	return func(c *config) { c.approvalPolicy = policy }
}

// WithSkipGitRepoCheck allows running Codex outside a Git repository.
func WithSkipGitRepoCheck() Option {
	return func(c *config) { c.skipGitRepoCheck = true }
}

// WithIgnoreUserConfig prevents loading $CODEX_HOME/config.toml.
func WithIgnoreUserConfig() Option {
	return func(c *config) { c.ignoreUserConfig = true }
}

// WithIgnoreRules prevents loading user or project execpolicy rules.
func WithIgnoreRules() Option {
	return func(c *config) { c.ignoreRules = true }
}

// reasoningEffortForThinkingLevel maps a per-call
// [ai.StreamOptions.ThinkingLevel] onto Codex's model_reasoning_effort
// scale (minimal/low/medium/high/xhigh). Codex has no "off":
// "off"/unknown return "" (omit the override); every other level maps
// through unchanged. "xhigh" is model-dependent, so Codex applies its
// own fallback when the active model does not support it.
func reasoningEffortForThinkingLevel(level ai.ThinkingLevel) string {
	switch level {
	case ai.ThinkingMinimal,
		ai.ThinkingLow,
		ai.ThinkingMedium,
		ai.ThinkingHigh,
		ai.ThinkingXHigh:
		return string(level)
	default:
		return ""
	}
}

// New creates a stateless Codex CLI provider.
func New(opts ...Option) *Provider {
	cfg := config{
		cliPath:        "codex",
		approvalPolicy: "never",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Provider{
		cfg:    cfg,
		sendFn: spawn,
	}
}

// Provider returns the provider identifier used by [ai.RegisterProvider].
func (p *Provider) Provider() string {
	return "codex-cli"
}

// StreamText runs a one-shot `codex exec --json --ephemeral` subprocess
// and streams [ai.Event]s extracted from its JSONL output.
//
// The Codex CLI emits completed items rather than token-level deltas, so
// each text or command-execution item produces a single start/end pair
// where the delta carries the full content. Only the last user message in
// [ai.Prompt.Messages] is sent; [ai.Prompt.System] is prefixed into the
// prompt because the CLI does not expose a dedicated system-prompt flag.
func (p *Provider) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	opts ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		cfg := p.cfg
		if model.ID != "" {
			cfg.model = model.ID
		}

		userText := lastUserText(prompt.Messages)
		if userText == "" {
			return nil, errors.New("codex: prompt has no user message")
		}

		args := sendArgs{
			prompt:          promptText(prompt.System, userText),
			reasoningEffort: reasoningEffortForThinkingLevel(opts.ThinkingLevel),
			ephemeral:       true,
		}

		stdout, cleanup, err := p.sendFn(ctx, cfg, args)
		if err != nil {
			return nil, err
		}

		push(ai.Event{Type: ai.EventStart})

		final, usage, pumpErr := pumpAIEvents(push, stdout, model, p.Provider(), cfg.model)

		cleanupErr := cleanup()
		if pumpErr == nil {
			pumpErr = cleanupErr
		}
		if pumpErr != nil {
			return final, pumpErr
		}

		if final == nil {
			return nil, nil
		}

		if final.Usage == (ai.Usage{}) {
			final.Usage = usage
		}
		if final.API == "" {
			final.API = p.Provider()
		}
		if final.Provider == "" {
			final.Provider = p.Provider()
		}
		if final.Model == "" {
			final.Model = cfg.model
		}

		return final, nil
	})
}

// GenerateObject runs `codex exec --json --output-schema <schema-file>` and
// returns the raw JSON text the model produced.
func (p *Provider) GenerateObject(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	schema *jsonschema.Schema,
	opts ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	cfg := p.cfg
	if model.ID != "" {
		cfg.model = model.ID
	}

	userText := lastUserText(prompt.Messages)
	if userText == "" {
		return nil, errors.New("codex: prompt has no user message")
	}
	if schema == nil {
		return nil, errors.New("codex: schema is required")
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal schema: %w", err)
	}

	schemaFile, err := os.CreateTemp("", "pi-go-codex-schema-*.json")
	if err != nil {
		return nil, fmt.Errorf("codex: create schema file: %w", err)
	}
	schemaPath := schemaFile.Name()
	defer os.Remove(schemaPath)

	if _, err := schemaFile.Write(schemaJSON); err != nil {
		_ = schemaFile.Close()
		return nil, fmt.Errorf("codex: write schema file: %w", err)
	}
	if err := schemaFile.Close(); err != nil {
		return nil, fmt.Errorf("codex: close schema file: %w", err)
	}

	args := sendArgs{
		prompt:           promptText(prompt.System, userText),
		reasoningEffort:  reasoningEffortForThinkingLevel(opts.ThinkingLevel),
		ephemeral:        true,
		outputSchemaPath: schemaPath,
	}

	stdout, cleanup, err := p.sendFn(ctx, cfg, args)
	if err != nil {
		return nil, err
	}

	raw, usage, parseErr := collectObjectResult(stdout)
	cleanupErr := cleanup()
	if parseErr != nil {
		return nil, parseErr
	}
	if cleanupErr != nil {
		return nil, cleanupErr
	}
	if raw == "" {
		return nil, errors.New("codex: empty object result")
	}

	return &ai.ObjectResponse{
		Raw:   raw,
		Usage: usage,
		Model: cfg.model,
	}, nil
}

const maxLineSize = 10 * 1024 * 1024 // 10MB

func lastUserText(msgs []ai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.RoleUser {
			return msgs[i].Text()
		}
	}
	return ""
}

func promptText(system, user string) string {
	if system == "" {
		return user
	}
	return strings.TrimSpace(system) + "\n\n" + user
}

func pumpAIEvents(
	push func(ai.Event),
	stdout io.Reader,
	model ai.Model,
	api string,
	modelName string,
) (*ai.Message, ai.Usage, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	msg := ai.Message{
		Role:       ai.RoleAssistant,
		API:        api,
		Model:      model.ID,
		StopReason: ai.StopReasonStop,
	}
	if msg.Model == "" {
		msg.Model = modelName
	}

	var (
		usage      ai.Usage
		resultErr  error
		contentIdx int
		itemIdx    = map[string]int{}
	)

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}

		switch line.Type {
		case "item.started":
			if line.Item.Type != "command_execution" {
				continue
			}
			idx := contentIdx
			contentIdx++
			itemIdx[line.Item.ID] = idx
			push(ai.Event{
				Type:         ai.EventToolStart,
				ContentIndex: idx,
			})

		case "item.completed":
			switch line.Item.Type {
			case "agent_message":
				if line.Item.Text == "" {
					continue
				}
				idx := contentIdx
				contentIdx++
				emitTextBlock(push, idx, line.Item.Text)
				setContent(&msg, idx, ai.Text{Text: line.Item.Text})

			case "command_execution":
				idx, ok := itemIdx[line.Item.ID]
				if !ok {
					idx = contentIdx
					contentIdx++
					push(ai.Event{
						Type:         ai.EventToolStart,
						ContentIndex: idx,
					})
				}
				call := commandToolCall(line.Item)
				push(ai.Event{
					Type:         ai.EventToolEnd,
					ContentIndex: idx,
					ToolCall:     &call,
				})
				setContent(&msg, idx, call)
			}

		case "turn.completed":
			if line.Usage != nil {
				usage = usageFromCodex(*line.Usage)
			}

		case "turn.failed", "error":
			resultErr = line.error()
		}
	}

	if err := scanner.Err(); err != nil {
		return &msg, usage, err
	}
	if resultErr != nil {
		return &msg, usage, resultErr
	}

	if len(msg.Content) == 0 {
		return nil, usage, nil
	}
	msg.Usage = usage
	return &msg, usage, nil
}

func emitTextBlock(push func(ai.Event), idx int, text string) {
	push(ai.Event{Type: ai.EventTextStart, ContentIndex: idx})
	push(ai.Event{
		Type:         ai.EventTextDelta,
		ContentIndex: idx,
		Delta:        text,
	})
	push(ai.Event{
		Type:         ai.EventTextEnd,
		ContentIndex: idx,
		Content:      text,
	})
}

func setContent(msg *ai.Message, idx int, c ai.Content) {
	for len(msg.Content) <= idx {
		msg.Content = append(msg.Content, ai.Text{})
	}
	msg.Content[idx] = c
}

func commandToolCall(item rawItem) ai.ToolCall {
	output := &ai.ServerToolOutput{
		Content: item.AggregatedOutput,
		IsError: item.commandFailed(),
	}
	if raw, err := json.Marshal(item); err == nil {
		output.Raw = raw
	}

	return ai.ToolCall{
		ID:   item.ID,
		Name: "bash",
		Arguments: map[string]any{
			"command": item.Command,
		},
		Server:     true,
		ServerType: ai.ServerToolBash,
		Output:     output,
	}
}

func collectObjectResult(stdout io.Reader) (string, ai.Usage, error) {
	msg, usage, err := pumpAIEvents(func(ai.Event) {}, stdout, ai.Model{}, "", "")
	if err != nil {
		return "", usage, err
	}
	if msg == nil {
		return "", usage, nil
	}
	return msg.Text(), usage, nil
}
