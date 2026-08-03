// Package claudecli provides an [ai.TextProvider] and [ai.ObjectProvider]
// implementation backed by the `claude` CLI (Claude Code) running
// in one-shot `--print --no-session-persistence` mode.
//
// Unlike the agent in pkg/agent/claude, this provider is stateless:
// each call spawns a fresh subprocess and writes nothing to disk. It
// is safe for concurrent use. Authentication follows the CLI's normal
// resolution order (OAuth session, ANTHROPIC_API_KEY, apiKeyHelper).
//
// Bind a model to the provider with [Provider.LanguageModel]:
//
//	lm := claudecli.New(claudecli.WithModel("sonnet")).LanguageModel(claudecli.ClaudeSonnet)
package claudecli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/sonnes/pi-go/pkg/ai"
)

// Compile-time interface checks.
var (
	_ ai.TextProvider   = (*Provider)(nil)
	_ ai.ObjectProvider = (*Provider)(nil)
)

// providerID is the Claude CLI provider identity.
const providerID = "claude-cli"

// Provider implements [ai.TextProvider] and [ai.ObjectProvider] by
// delegating each call to a fresh `claude --print` subprocess.
type Provider struct {
	cfg config

	// sendFn spawns the subprocess. Defaults to [spawn]; overridden in tests.
	sendFn func(ctx context.Context, cfg config, args sendArgs) (io.ReadCloser, func() error, error)
}

// config holds all configuration for the provider.
type config struct {
	cliPath         string
	workDir         string
	addDirs         []string
	env             []string
	allowedTools    []string
	maxTurns        int
	maxBudgetUSD    float64
	model           string
	sessionID       string
	partialMessages bool
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

// WithMaxBudgetUSD caps the API spend for each print-mode invocation.
// A value less than or equal to zero leaves Claude Code's default unlimited.
func WithMaxBudgetUSD(limit float64) Option {
	return func(c *config) { c.maxBudgetUSD = limit }
}

// WithPartialMessages streams Claude Code's partial content-block events.
// Without this option, the provider emits one complete event triple per block.
func WithPartialMessages() Option {
	return func(c *config) { c.partialMessages = true }
}

// WithSessionID continues a previous Claude Code conversation via
// `--resume`, and keeps the subprocess persisting the session so a later
// call can resume it in turn. The ID must be one the CLI issued.
//
// This is deliberately separate from [ai.WithSessionID], which is a
// prompt-cache affinity key that providers are free to invent or reuse —
// passing one of those here would resume a session that does not exist.
func WithSessionID(id string) Option {
	return func(c *config) { c.sessionID = id }
}

// WithModel overrides the default model. Per-call [ai.Model.ID] values
// take precedence over this setting.
func WithModel(model string) Option {
	return func(c *config) { c.model = model }
}

// effortForThinkingLevel maps a per-call [ai.StreamOptions.ThinkingLevel]
// onto the Claude CLI's --effort scale (low/medium/high/xhigh/max). The
// CLI has no "off" or "minimal" effort: "off"/unknown return "" (omit the
// flag) and "minimal" floors to "low". No thinking level maps to "max".
func effortForThinkingLevel(level ai.ThinkingLevel) string {
	switch level {
	case ai.ThinkingMinimal, ai.ThinkingLow:
		return string(ai.ThinkingLow)
	case ai.ThinkingMedium:
		return string(ai.ThinkingMedium)
	case ai.ThinkingHigh:
		return string(ai.ThinkingHigh)
	case ai.ThinkingXHigh:
		return string(ai.ThinkingXHigh)
	default:
		return ""
	}
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

// ID returns the provider identity.
func (p *Provider) ID() string {
	return providerID
}

// StreamText runs a one-shot `claude --print` subprocess and streams
// [ai.Event]s extracted from its NDJSON output.
//
// The Claude CLI emits whole messages rather than token-level deltas,
// so each text/thinking/tool_use block produces a single
// start/delta/end triple where the delta carries the full content.
//
// Only the last user message in [ai.Prompt.Messages] is sent; prior turns
// are not replayed. A call is stateless by default — to continue an earlier
// conversation, resume it with [WithSessionID] instead of sending history.
// [ai.Prompt.System] maps to `--system-prompt`.
// [ai.Prompt.Tools], [ai.StreamOptions.Temperature], and
// [ai.StreamOptions.MaxTokens] are not exposed by the CLI and are ignored.
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
			return nil, errors.New("claude: prompt has no user message")
		}

		args := sendArgs{
			prompt:          userText,
			systemPrompt:    prompt.System,
			effort:          effortForThinkingLevel(opts.ThinkingLevel),
			noPersistence:   cfg.sessionID == "",
			resumeSession:   cfg.sessionID,
			partialMessages: cfg.partialMessages,
		}

		stdout, cleanup, err := p.sendFn(ctx, cfg, args)
		if err != nil {
			return nil, err
		}

		push(ai.Event{Type: ai.EventStart})

		final, usage, pumpErr := pumpAIEvents(
			push,
			stdout,
			model,
			cfg.partialMessages,
		)

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
			final.API = p.ID()
		}
		if final.Provider == "" {
			final.Provider = p.ID()
		}
		if final.Model == "" {
			final.Model = cfg.model
		}

		return final, nil
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
	opts ai.StreamOptions,
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
		effort:        effortForThinkingLevel(opts.ThinkingLevel),
		noPersistence: cfg.sessionID == "",
		resumeSession: cfg.sessionID,
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
	partialMessages bool,
) (*ai.Message, ai.Usage, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var (
		lastAssistant *ai.Message
		usage         ai.Usage
		resultErr     error
		contentIdx    int
		partial       partialBlocks
	)

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}
		switch line.Type {
		case "stream_event":
			if partialMessages {
				partial.handle(push, line.Event, &contentIdx)
			}

		case "assistant":
			var msg anthropicMessage
			if err := json.Unmarshal(line.Message, &msg); err != nil {
				continue
			}
			aiMsg := toAIMessage(msg)
			aiMsg.Model = model.ID
			skipped := partial.finish(push, &contentIdx)
			emitContentBlocks(push, aiMsg.Content, &contentIdx, skipped)
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

// partialBlocks translates Claude Code's embedded Anthropic SSE events into
// ai.Events and suppresses the equivalent complete blocks that follow on the
// assistant line.
type partialBlocks struct {
	blocks map[int]*partialBlock
}

type partialBlock struct {
	contentIndex int
	kind         string
	text         strings.Builder
	call         ai.ToolCall
}

func (p *partialBlocks) handle(
	push func(ai.Event),
	raw json.RawMessage,
	contentIdx *int,
) {
	var event streamEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return
	}

	switch event.Type {
	case "content_block_start":
		p.start(push, event, contentIdx)
	case "content_block_delta":
		p.delta(push, event)
	case "content_block_stop":
		p.close(push, event.Index, contentIdx)
	}
}

func (p *partialBlocks) start(
	push func(ai.Event),
	event streamEvent,
	contentIdx *int,
) {
	if event.ContentBlock == nil {
		return
	}
	if p.blocks == nil {
		p.blocks = make(map[int]*partialBlock)
	}
	if _, exists := p.blocks[event.Index]; exists {
		return
	}

	block := &partialBlock{
		contentIndex: *contentIdx,
		kind:         event.ContentBlock.Type,
		call: ai.ToolCall{
			ID:        event.ContentBlock.ID,
			Name:      event.ContentBlock.Name,
			Arguments: event.ContentBlock.Input,
		},
	}
	p.blocks[event.Index] = block

	switch block.kind {
	case "text":
		push(ai.Event{Type: ai.EventTextStart, ContentIndex: block.contentIndex})
	case "thinking":
		push(ai.Event{Type: ai.EventThinkStart, ContentIndex: block.contentIndex})
	case "tool_use":
		call := block.call
		push(ai.Event{
			Type:         ai.EventToolStart,
			ContentIndex: block.contentIndex,
			ToolCall:     &call,
		})
	}
}

func (p *partialBlocks) delta(push func(ai.Event), event streamEvent) {
	if event.Delta == nil {
		return
	}
	block := p.blocks[event.Index]
	if block == nil {
		return
	}

	switch event.Delta.Type {
	case "text_delta":
		block.text.WriteString(event.Delta.Text)
		push(ai.Event{
			Type:         ai.EventTextDelta,
			ContentIndex: block.contentIndex,
			Delta:        event.Delta.Text,
		})
	case "thinking_delta":
		block.text.WriteString(event.Delta.Thinking)
		push(ai.Event{
			Type:         ai.EventThinkDelta,
			ContentIndex: block.contentIndex,
			Delta:        event.Delta.Thinking,
		})
	case "input_json_delta":
		block.text.WriteString(event.Delta.PartialJSON)
		push(ai.Event{
			Type:         ai.EventToolDelta,
			ContentIndex: block.contentIndex,
			Delta:        event.Delta.PartialJSON,
		})
	}
}

func (p *partialBlocks) close(
	push func(ai.Event),
	sourceIndex int,
	contentIdx *int,
) {
	block := p.blocks[sourceIndex]
	if block == nil {
		return
	}

	content := block.text.String()
	switch block.kind {
	case "text":
		push(ai.Event{
			Type:         ai.EventTextEnd,
			ContentIndex: block.contentIndex,
			Content:      content,
		})
	case "thinking":
		push(ai.Event{
			Type:         ai.EventThinkEnd,
			ContentIndex: block.contentIndex,
			Content:      content,
		})
	case "tool_use":
		if content != "" {
			_ = json.Unmarshal([]byte(content), &block.call.Arguments)
		}
		call := block.call
		push(ai.Event{
			Type:         ai.EventToolEnd,
			ContentIndex: block.contentIndex,
			ToolCall:     &call,
		})
	}

	if block.contentIndex >= *contentIdx {
		*contentIdx = block.contentIndex + 1
	}
	delete(p.blocks, sourceIndex)
}

func (p *partialBlocks) finish(
	push func(ai.Event),
	contentIdx *int,
) map[int]bool {
	if len(p.blocks) == 0 {
		return nil
	}

	indices := make([]int, 0, len(p.blocks))
	for index := range p.blocks {
		indices = append(indices, index)
	}
	sort.Ints(indices)

	skipped := make(map[int]bool, len(indices))
	for _, index := range indices {
		skipped[index] = true
		p.close(push, index, contentIdx)
	}

	return skipped
}

// emitContentBlocks pushes ai.Events for each content block in a
// completed assistant message. Because the Claude CLI doesn't stream
// token deltas, each block emits a start/delta/end triple where the
// delta carries the full content.
func emitContentBlocks(
	push func(ai.Event),
	blocks []ai.Content,
	idx *int,
	skipped map[int]bool,
) {
	for sourceIndex, block := range blocks {
		if skipped[sourceIndex] {
			continue
		}
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
