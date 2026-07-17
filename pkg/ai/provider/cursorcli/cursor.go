package cursorcli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/sonnes/pi-go/pkg/ai"
)

var _ ai.TextProvider = (*Provider)(nil)

// providerID is the Cursor CLI provider identity.
const providerID = "cursor-cli"

// Provider implements [ai.TextProvider] by delegating each call to a fresh
// `cursor-agent --print` subprocess.
type Provider struct {
	cfg config

	// sendFn spawns the subprocess. Defaults to [spawn]; overridden in tests.
	sendFn func(ctx context.Context, cfg config, args sendArgs) (io.ReadCloser, func() error, error)
}

type config struct {
	cliPath     string
	workDir     string
	env         []string
	apiKey      string
	headers     []string
	model       string
	mode        string
	sandbox     string
	force       bool
	approveMCPs bool
	browser     bool
}

// Option configures a [Provider].
type Option func(*config)

// WithCLIPath sets the path to the cursor-agent CLI binary. Defaults to
// "cursor-agent".
func WithCLIPath(path string) Option {
	return func(c *config) { c.cliPath = path }
}

// WithWorkDir sets the workspace directory for each subprocess.
func WithWorkDir(dir string) Option {
	return func(c *config) { c.workDir = dir }
}

// WithEnv sets additional environment variables for each subprocess.
// Each entry should be in "KEY=VALUE" format.
func WithEnv(env ...string) Option {
	return func(c *config) { c.env = env }
}

// WithAPIKey passes a Cursor API key via --api-key. Most callers should prefer
// CURSOR_API_KEY or `cursor-agent login`.
func WithAPIKey(key string) Option {
	return func(c *config) { c.apiKey = key }
}

// WithHeaders adds custom request headers in "Name: Value" form.
func WithHeaders(headers ...string) Option {
	return func(c *config) { c.headers = headers }
}

// WithModel overrides the default model. Per-call [ai.Model.ID] values take
// precedence over this setting.
func WithModel(model string) Option {
	return func(c *config) { c.model = model }
}

// WithMode sets Cursor's execution mode. Defaults to "ask" for read-only
// provider-style calls. Use an empty mode to let Cursor use its default agent
// behavior.
func WithMode(mode string) Option {
	return func(c *config) { c.mode = mode }
}

// WithSandbox sets Cursor's sandbox mode, such as "enabled" or "disabled".
func WithSandbox(mode string) Option {
	return func(c *config) { c.sandbox = mode }
}

// WithForce passes --force so Cursor can run allowed commands without
// interactive approval.
func WithForce() Option {
	return func(c *config) { c.force = true }
}

// WithApproveMCPs passes --approve-mcps for headless MCP approval.
func WithApproveMCPs() Option {
	return func(c *config) { c.approveMCPs = true }
}

// WithBrowser enables Cursor CLI browser automation support.
func WithBrowser() Option {
	return func(c *config) { c.browser = true }
}

// New creates a stateless Cursor CLI provider.
func New(opts ...Option) *Provider {
	cfg := config{
		cliPath: "cursor-agent",
		mode:    "ask",
	}
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

// StreamText runs a one-shot `cursor-agent --print` subprocess and streams
// [ai.Event]s extracted from its NDJSON output.
//
// The Cursor CLI emits assistant text deltas and provider-executed tool calls.
// Only the last user message in [ai.Prompt.Messages] is sent; [ai.Prompt.System]
// is prefixed into the prompt because the CLI has no dedicated system prompt
// flag. [ai.StreamOptions.ThinkingLevel] is ignored: the Cursor CLI has no
// reasoning-effort flag and binds reasoning to the model name (for example
// "sonnet-4.5-thinking") instead.
func (p *Provider) StreamText(
	ctx context.Context,
	model ai.Model,
	prompt ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		cfg := p.cfg
		if model.ID != "" {
			cfg.model = model.ID
		}

		userText := lastUserText(prompt.Messages)
		if userText == "" {
			return nil, errors.New("cursor: prompt has no user message")
		}

		args := sendArgs{
			prompt: promptText(prompt.System, userText),
		}

		stdout, cleanup, err := p.sendFn(ctx, cfg, args)
		if err != nil {
			return nil, err
		}

		push(ai.Event{Type: ai.EventStart})

		final, pumpErr := pumpAIEvents(push, stdout, model, p.ID(), cfg.model)

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
) (*ai.Message, error) {
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
		resultErr   error
		contentIdx  int
		textIdx     int
		textOpen    bool
		text        strings.Builder
		toolIndexes = map[string]int{}
	)

	closeText := func() {
		if !textOpen {
			return
		}
		content := text.String()
		push(ai.Event{
			Type:         ai.EventTextEnd,
			ContentIndex: textIdx,
			Content:      content,
		})
		setContent(&msg, textIdx, ai.Text{Text: content})
		textOpen = false
	}

	emitText := func(delta string) {
		if delta == "" {
			return
		}
		if !textOpen {
			text.Reset()
			textIdx = contentIdx
			contentIdx++
			textOpen = true
			push(ai.Event{
				Type:         ai.EventTextStart,
				ContentIndex: textIdx,
			})
		}
		text.WriteString(delta)
		push(ai.Event{
			Type:         ai.EventTextDelta,
			ContentIndex: textIdx,
			Delta:        delta,
		})
	}

	for scanner.Scan() {
		line, err := parseLine(scanner.Bytes())
		if err != nil {
			continue
		}

		switch line.Type {
		case "assistant":
			emitText(line.Message.text())

		case "tool_call":
			info, ok := line.toolInfo()
			if !ok {
				continue
			}
			switch line.Subtype {
			case "started":
				closeText()
				idx := contentIdx
				contentIdx++
				toolIndexes[info.ID] = idx
				push(ai.Event{
					Type:         ai.EventToolStart,
					ContentIndex: idx,
				})
			case "completed":
				closeText()
				idx, ok := toolIndexes[info.ID]
				if !ok {
					idx = contentIdx
					contentIdx++
					push(ai.Event{
						Type:         ai.EventToolStart,
						ContentIndex: idx,
					})
				}
				call := commandToolCall(info)
				push(ai.Event{
					Type:         ai.EventToolEnd,
					ContentIndex: idx,
					ToolCall:     &call,
				})
				setContent(&msg, idx, call)
			}

		case "result":
			if line.IsError {
				resultErr = line.err()
				continue
			}
			if !textOpen && len(msg.Content) == 0 && line.Result != "" {
				emitText(line.Result)
			}
		}
	}

	closeText()

	if err := scanner.Err(); err != nil {
		return &msg, err
	}
	if resultErr != nil {
		return &msg, resultErr
	}
	if len(msg.Content) == 0 {
		return nil, nil
	}
	return &msg, nil
}

func setContent(msg *ai.Message, idx int, c ai.Content) {
	for len(msg.Content) <= idx {
		msg.Content = append(msg.Content, ai.Text{})
	}
	msg.Content[idx] = c
}

func commandToolCall(info toolInfo) ai.ToolCall {
	output := &ai.ServerToolOutput{
		Content: info.Result,
		Raw:     info.Raw,
		IsError: info.IsError,
	}

	return ai.ToolCall{
		ID:         info.ID,
		Name:       info.Name,
		Arguments:  info.Args,
		Server:     true,
		ServerType: info.ServerType,
		Output:     output,
	}
}
