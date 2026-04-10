// Package claudecli provides an [ai.Provider] and [ai.ObjectProvider]
// implementation backed by the `claude` CLI (Claude Code) running
// in one-shot `--print --no-session-persistence` mode.
//
// Unlike the agent in pkg/agent/claude, this provider is stateless:
// each call spawns a fresh subprocess and writes nothing to disk. It
// is safe for concurrent use. Authentication follows the CLI's normal
// resolution order (OAuth session, ANTHROPIC_API_KEY, apiKeyHelper).
//
// Registration:
//
//	ai.RegisterProvider("claude-cli", claudecli.New(claudecli.WithModel("sonnet")))
package claudecli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Compile-time interface checks.
var (
	_ ai.Provider       = (*Provider)(nil)
	_ ai.ObjectProvider = (*Provider)(nil)
)

// Provider implements [ai.Provider] and [ai.ObjectProvider] by
// delegating each call to a fresh `claude --print` subprocess.
type Provider struct {
	cfg config

	// sendFn spawns the subprocess. Defaults to [spawn]; overridden in tests.
	sendFn func(ctx context.Context, cfg config, args sendArgs) (io.ReadCloser, func() error, error)
}

// config holds all configuration for the provider.
type config struct {
	cliPath      string
	workDir      string
	addDirs      []string
	env          []string
	allowedTools []string
	maxTurns     int
	model        string
}

// Option configures a [Provider].
type Option func(*config)

// WithCLIPath sets the path to the claude CLI binary. Defaults to "claude".
func WithCLIPath(path string) Option {
	return func(c *config) { c.cliPath = path }
}

// WithWorkDir sets the working directory for each subprocess.
func WithWorkDir(dir string) Option {
	return func(c *config) { c.workDir = dir }
}

// WithAddDirs adds additional working directories via --add-dir flags.
func WithAddDirs(dirs ...string) Option {
	return func(c *config) { c.addDirs = dirs }
}

// WithEnv sets additional environment variables for each subprocess.
// Each entry should be in "KEY=VALUE" format.
func WithEnv(env ...string) Option {
	return func(c *config) { c.env = env }
}

// WithAllowedTools sets the tools the subprocess is allowed to use
// via the --allowedTools flag.
func WithAllowedTools(tools ...string) Option {
	return func(c *config) { c.allowedTools = tools }
}

// WithMaxTurns limits the number of agentic turns via --max-turns.
func WithMaxTurns(n int) Option {
	return func(c *config) { c.maxTurns = n }
}

// WithModel overrides the default model. Per-call [ai.Model.ID] values
// take precedence over this setting.
func WithModel(model string) Option {
	return func(c *config) { c.model = model }
}

// New creates a stateless Claude CLI provider.
func New(opts ...Option) *Provider {
	cfg := config{cliPath: "claude"}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Provider{
		cfg:    cfg,
		sendFn: spawn,
	}
}

// API returns the provider identifier used by [ai.RegisterProvider].
func (p *Provider) API() string {
	return "claude-cli"
}

// StreamText runs a one-shot `claude --print` subprocess and streams
// [ai.Event]s extracted from its NDJSON output.
//
// The Claude CLI emits whole messages rather than token-level deltas,
// so each text/thinking/tool_use block produces a single
// start/delta/end triple where the delta carries the full content.
//
// Only the last user message in [ai.Prompt.Messages] is sent; prior
// turns cannot be replayed without breaking the stateless,
// no-persistence guarantee. [ai.Prompt.System] maps to `--system-prompt`.
// [ai.Prompt.Tools], [ai.StreamOptions.Temperature], and
// [ai.StreamOptions.MaxTokens] are not exposed by the CLI and are ignored.
func (p *Provider) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) {
		cfg := p.cfg
		if model.ID != "" {
			cfg.model = model.ID
		}

		userText := lastUserText(prompt.Messages)
		if userText == "" {
			push(ai.Event{
				Type: ai.EventError,
				Err:  errors.New("claude: prompt has no user message"),
			})
			return
		}

		args := sendArgs{
			prompt:        userText,
			systemPrompt:  prompt.System,
			noPersistence: true,
		}

		stdout, cleanup, err := p.sendFn(ctx, cfg, args)
		if err != nil {
			push(ai.Event{Type: ai.EventError, Err: err})
			return
		}

		push(ai.Event{Type: ai.EventStart})

		final, usage, pumpErr := pumpAIEvents(push, stdout, model)

		cleanupErr := cleanup()
		if pumpErr == nil {
			pumpErr = cleanupErr
		}

		if pumpErr != nil {
			push(ai.Event{Type: ai.EventError, Err: pumpErr, Message: final})
			return
		}

		if final == nil {
			push(ai.Event{Type: ai.EventDone})
			return
		}

		if final.Usage == (ai.Usage{}) {
			final.Usage = usage
		}
		if final.API == "" {
			final.API = p.API()
		}
		if final.Model == "" {
			final.Model = cfg.model
		}

		push(ai.Event{
			Type:       ai.EventDone,
			Message:    final,
			StopReason: final.StopReason,
		})
	})
}

// GenerateObject runs a one-shot `claude --print --json-schema <schema>`
// subprocess and returns the raw JSON text the model produced. The
// caller (typically [ai.GenerateObject]) is responsible for unmarshaling
// the raw text into the target type.
//
// The schema is passed verbatim to the CLI via `--json-schema`, which
// enforces structured output validation. Only the last user message in
// [ai.Prompt.Messages] is sent.
func (p *Provider) GenerateObject(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	schema *jsonschema.Schema,
	_ ai.StreamOptions,
) (*ai.ObjectResponse, error) {
	cfg := p.cfg
	if model.ID != "" {
		cfg.model = model.ID
	}

	userText := lastUserText(prompt.Messages)
	if userText == "" {
		return nil, errors.New("claude: prompt has no user message")
	}

	if schema == nil {
		return nil, errors.New("claude: schema is required")
	}
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("claude: marshal schema: %w", err)
	}

	args := sendArgs{
		prompt:        userText,
		systemPrompt:  prompt.System,
		noPersistence: true,
		jsonSchema:    string(schemaJSON),
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
		return nil, errors.New("claude: empty object result")
	}

	return &ai.ObjectResponse{
		Raw:   raw,
		Usage: usage,
		Model: cfg.model,
	}, nil
}

// --- internals ---

const maxLineSize = 10 * 1024 * 1024 // 10MB

// lastUserText returns the text of the last user message in msgs, or
// the empty string if none exists.
func lastUserText(msgs []ai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.RoleUser {
			return msgs[i].Text()
		}
	}
	return ""
}

// pumpAIEvents reads NDJSON lines from stdout, translating Claude CLI
// output into [ai.Event]s via the push callback.
func pumpAIEvents(
	push func(ai.Event),
	stdout io.Reader,
	model ai.Model,
) (*ai.Message, ai.Usage, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var (
		lastAssistant *ai.Message
		usage         ai.Usage
		resultErr     error
		contentIdx    int
	)

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}
		switch line.Type {
		case "assistant":
			var msg anthropicMessage
			if err := json.Unmarshal(line.Message, &msg); err != nil {
				continue
			}
			aiMsg := toAIMessage(msg)
			aiMsg.Model = model.ID
			emitContentBlocks(push, aiMsg.Content, &contentIdx)
			m := aiMsg
			lastAssistant = &m

		case "result":
			if line.Usage != nil {
				usage = ai.Usage{
					Input:      line.Usage.InputTokens,
					Output:     line.Usage.OutputTokens,
					CacheRead:  line.Usage.CacheReadInputTokens,
					CacheWrite: line.Usage.CacheCreationInputTokens,
					Total:      line.Usage.InputTokens + line.Usage.OutputTokens,
				}
			}
			if line.CostUSD > 0 {
				usage.Cost.Total = line.CostUSD
			}
			if line.IsError {
				resultErr = fmt.Errorf("claude: %s", line.Result)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return lastAssistant, usage, err
	}
	return lastAssistant, usage, resultErr
}

// emitContentBlocks pushes ai.Events for each content block in a
// completed assistant message. Because the Claude CLI doesn't stream
// token deltas, each block emits a start/delta/end triple where the
// delta carries the full content.
func emitContentBlocks(push func(ai.Event), blocks []ai.Content, idx *int) {
	for _, block := range blocks {
		switch b := block.(type) {
		case ai.Text:
			push(ai.Event{Type: ai.EventTextStart, ContentIndex: *idx})
			push(ai.Event{
				Type:         ai.EventTextDelta,
				ContentIndex: *idx,
				Delta:        b.Text,
			})
			push(ai.Event{
				Type:         ai.EventTextEnd,
				ContentIndex: *idx,
				Content:      b.Text,
			})
			*idx++
		case ai.Thinking:
			push(ai.Event{Type: ai.EventThinkStart, ContentIndex: *idx})
			push(ai.Event{
				Type:         ai.EventThinkDelta,
				ContentIndex: *idx,
				Delta:        b.Thinking,
			})
			push(ai.Event{Type: ai.EventThinkEnd, ContentIndex: *idx})
			*idx++
		case ai.ToolCall:
			call := b
			push(ai.Event{Type: ai.EventToolStart, ContentIndex: *idx})
			push(ai.Event{
				Type:         ai.EventToolEnd,
				ContentIndex: *idx,
				ToolCall:     &call,
			})
			*idx++
		}
	}
}

// collectObjectResult reads NDJSON lines from stdout and returns the
// final JSON text. It prefers the `result` line's Result field and
// falls back to the concatenated text of the last assistant message.
func collectObjectResult(stdout io.Reader) (string, ai.Usage, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var (
		resultText    string
		lastAssistant string
		usage         ai.Usage
		resultErr     error
	)

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}
		switch line.Type {
		case "assistant":
			var msg anthropicMessage
			if err := json.Unmarshal(line.Message, &msg); err != nil {
				continue
			}
			var sb strings.Builder
			for _, c := range msg.Content {
				if c.Type == "text" {
					sb.WriteString(c.Text)
				}
			}
			if s := sb.String(); s != "" {
				lastAssistant = s
			}

		case "result":
			if line.Usage != nil {
				usage = ai.Usage{
					Input:      line.Usage.InputTokens,
					Output:     line.Usage.OutputTokens,
					CacheRead:  line.Usage.CacheReadInputTokens,
					CacheWrite: line.Usage.CacheCreationInputTokens,
					Total:      line.Usage.InputTokens + line.Usage.OutputTokens,
				}
			}
			if line.CostUSD > 0 {
				usage.Cost.Total = line.CostUSD
			}
			if line.IsError {
				resultErr = fmt.Errorf("claude: %s", line.Result)
				continue
			}
			if line.Result != "" {
				resultText = line.Result
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", usage, err
	}
	if resultErr != nil {
		return "", usage, resultErr
	}

	if resultText != "" {
		return resultText, usage, nil
	}
	return lastAssistant, usage, nil
}
