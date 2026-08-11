package prompt

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/session"
)

// defaultIdentity is the opening of the prompt: what the agent is and
// when it stops.
const defaultIdentity = "You are an agent. Work the task through to completion with the tools you have, " +
	"and stop when it is done rather than asking what to do next."

// Default is the system prompt builder the harness uses when none is
// configured. It writes, in order: identity, the tool listing, the skill
// catalog, and the root-scoped instructions.
//
// A section with nothing in it is omitted. A harness with no skills
// therefore produces a prompt that never mentions skills.
//
// Three things are deliberately absent:
//
//   - Directory-bound instructions, which govern a directory the agent
//     does not always work in. Nothing adds them later, so a builder
//     that wants one writes it from [Env.Instructions].
//   - Environment facts, which change between sessions and make the
//     prompt a poor cache key. They belong to [DefaultSeed].
//   - [Env.Agents], because no tool runs a definition. A list of them
//     invites the model to hallucinate a way to run one.
func Default(_ context.Context, env *Env) (string, error) {
	var b strings.Builder

	b.WriteString(defaultIdentity)

	if len(env.Tools) > 0 {
		section(&b, "Tools", "")
		for _, t := range env.Tools {
			bullet(&b, t.Name, toolHint(t))
		}
	}

	if len(env.Skills) > 0 {
		section(&b, "Skills",
			"Skills are instructions you load on demand with the `skill` tool. "+
				"Load the one that covers the task before starting it.")
		for _, s := range env.Skills {
			bullet(&b, s.Name, s.Description)
		}
	}

	// Default leaves a directory-bound document to the caller. It governs
	// a directory this agent does not always work in, and the decision
	// about when it applies is a judgment the harness cannot make. See
	// [def.Instructions.Dir].
	if docs := unbound(env.Instructions); len(docs) > 0 {
		section(&b, "Instructions", "")
		for _, doc := range docs {
			b.WriteString("\n### ")
			b.WriteString(doc.Source)
			b.WriteString("\n\n")
			b.WriteString(strings.TrimSpace(doc.Content))
			b.WriteString("\n")
		}
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

// DefaultSeed is the seeder the harness uses when none is configured.
// It emits one meta entry that describes the environment the session
// started in — working directory, platform, date.
//
// These are facts, not identity, so they are here and not in [Default].
// They are true when the session begins, and they are worth a place in
// its history. In a system prompt they churn a value that providers
// cache.
func DefaultSeed(_ context.Context, env *Env) ([]session.Entry, error) {
	text := fmt.Sprintf(
		"<environment>\nWorking directory: %s\nPlatform: %s\nDate: %s\n</environment>",
		env.WorkDir,
		runtime.GOOS,
		time.Now().Format("2006-01-02"),
	)
	entry := session.NewMessageEntry(ai.UserMessage(text))
	entry.Meta = true
	return []session.Entry{entry}, nil
}

// NoSeed is the [Seeder] that seeds nothing. A nil seeder given to the
// harness means "use the default", which is [DefaultSeed]. A caller who
// wants a fresh session to start empty passes NoSeed instead.
func NoSeed(context.Context, *Env) ([]session.Entry, error) {
	return nil, nil
}

// toolHint returns the one-sentence UseWhen. If UseWhen is empty, it
// returns the first line of the full description, so a long tool
// document does not swamp the prompt.
func toolHint(t ai.ToolInfo) string {
	if t.UseWhen != "" {
		return t.UseWhen
	}
	first, _, _ := strings.Cut(t.Description, "\n")
	return first
}

// unbound returns the documents that govern the whole tree rather than
// one directory in it.
func unbound(docs []def.Instructions) []def.Instructions {
	out := make([]def.Instructions, 0, len(docs))
	for _, d := range docs {
		if d.Dir == "" {
			out = append(out, d)
		}
	}
	return out
}

func section(b *strings.Builder, title, intro string) {
	b.WriteString("\n\n## ")
	b.WriteString(title)
	b.WriteString("\n")
	if intro != "" {
		b.WriteString("\n")
		b.WriteString(intro)
		b.WriteString("\n")
	}
}

func bullet(b *strings.Builder, name, desc string) {
	b.WriteString("\n- ")
	b.WriteString(name)
	if desc != "" {
		b.WriteString(": ")
		b.WriteString(desc)
	}
}
