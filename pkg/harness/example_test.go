// These examples document the intended developer experience of the
// harness package. Example has an Output comment and is verified by
// go test; the others compile and run without comparing output.
package harness_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/durable"
	"github.com/sonnes/pi-go/pkg/harness"
	"github.com/sonnes/pi-go/pkg/harness/def"
	"github.com/sonnes/pi-go/pkg/harness/fs"
	"github.com/sonnes/pi-go/pkg/session"
)

// ProjectState is the example session state.
type ProjectState struct {
	Title string
}

// Example shows the whole composition: a project directory read by
// convention, a skill invoked on demand, and the default prompt
// builders wiring it all into one loop.
func Example() {
	ctx := context.Background()

	// A project laid out by convention. In a real program this is the
	// user's repository, not something the example writes.
	dir := exampleProject()
	defer os.RemoveAll(dir)

	// One resolver per artifact kind, each pointed at the directory the
	// convention puts it in.
	proj := os.DirFS(dir)

	h, err := harness.New[ProjectState](
		harness.WithCatalog(exampleCatalog()),
		harness.WithDefaultModel("mock/small"),
		harness.WithWorkDir(dir),
		harness.WithTools(exampleReadTool()),
		// Sources are listed lowest first, so a skill in the project
		// overrides one declared here in code.
		harness.WithSkills(
			def.Skills(def.Skill{
				Name:        "summarize",
				Description: "Summarizes a long document.",
				Body:        "Summarize in three sentences.",
			}),
			fs.Skills(proj, ".agents/skills"),
		),
		harness.WithInstructions(fs.Instructions(proj, "AGENTS.md")),
	)
	if err != nil {
		panic(err)
	}

	// The session ID decides what is resumed. A fresh one is seeded
	// with the environment block on its first run; a resumed one
	// already has it in history.
	a, err := h.Agent(ctx, durable.WithSessionID("ticket-8472"))
	if err != nil {
		panic(err)
	}
	defer a.Close()

	msgs, err := a.Run(ctx, durable.Text("Commit the change in web/.")).Wait()
	if err != nil {
		panic(err)
	}
	fmt.Println(msgs[len(msgs)-1].Text())

	// The transcript holds what a user should see — the seeded
	// environment block stays hidden.
	transcript, err := a.Transcript(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("transcript entries:", len(transcript))

	// Output:
	// Committed with an imperative subject.
	// transcript entries: 4
}

// ExampleHarness_Agent shows the minimum: a catalog, a default model,
// and one skill declared in code.
func ExampleHarness_Agent() {
	ctx := context.Background()

	h, err := harness.New[ProjectState](
		harness.WithCatalog(exampleCatalog()),
		harness.WithDefaultModel("mock/small"),
		harness.WithSkills(def.Skills(def.Skill{
			Name:        "review",
			Description: "Reviews a diff for defects.",
			Body:        "Check every hunk for defects before approving.",
		})),
	)
	if err != nil {
		panic(err)
	}

	a, err := h.Agent(ctx)
	if err != nil {
		panic(err)
	}
	defer a.Close()
}

// ExampleHarness_Agent_perBuild shows one harness serving many
// repositories. The baseline holds what the process knows; each session
// adds its own project on top, overriding by name what it redefines and
// inheriting the rest.
func ExampleHarness_Agent_perBuild() {
	ctx := context.Background()

	h, err := harness.New[ProjectState](
		harness.WithCatalog(exampleCatalog()),
		harness.WithDefaultModel("mock/small"),
		// The user's own skills, available to every session.
		harness.WithSkills(fs.Skills(os.DirFS(userConfigDir()), "skills")),
	)
	if err != nil {
		panic(err)
	}

	repo := exampleProject()
	defer os.RemoveAll(repo)

	a, err := h.Agent(ctx,
		durable.WithSessionID("ticket-8472"),
		harness.WithWorkDir(repo),
		// This session's project, layered above the user's.
		harness.WithSkills(fs.Skills(os.DirFS(repo), ".agents/skills")),
		harness.WithTools(exampleReadTool()),
	)
	if err != nil {
		panic(err)
	}
	defer a.Close()
}

// ExampleWithMiddleware shows per-run context that must not persist: a
// reminder computed from live state, injected as an ephemeral entry
// that the model reads once and the store never sees.
//
// Middleware belongs to [durable], and the harness forwards the option
// down untouched along with everything else in the flat list.
func ExampleWithMiddleware() {
	remind := func(next durable.Runner) durable.Runner {
		return func(ctx context.Context, entries ...session.Entry) *durable.Stream {
			note := durable.Ephemeral(durable.Text("Reminder: the build is currently red."))
			return next(ctx, append([]session.Entry{note}, entries...)...)
		}
	}

	_, err := harness.New[ProjectState](
		harness.WithCatalog(exampleCatalog()),
		harness.WithDefaultModel("mock/small"),
		durable.WithMiddleware(remind),
	)
	if err != nil {
		panic(err)
	}
}

// --- example fixtures ---

// exampleProject writes a small .agents project: one skill, one
// instructions document.
func exampleProject() string {
	dir, err := os.MkdirTemp("", "harness-example")
	if err != nil {
		panic(err)
	}
	files := map[string]string{
		".agents/skills/commit/SKILL.md": "---\n" +
			"name: commit\n" +
			"description: Writes a commit message.\n" +
			"---\n" +
			"Write the subject in the imperative mood.\n",
		"AGENTS.md": "Run make test before claiming anything works.\n",
	}
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			panic(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			panic(err)
		}
	}
	return dir
}

func exampleReadTool() ai.Tool {
	type input struct {
		Path string `json:"path"`
	}
	return ai.DefineTool(
		"read",
		"Read a file from the working directory.",
		func(_ context.Context, in input) (string, error) {
			return "contents of " + in.Path, nil
		},
	).WithUseWhen("You need the contents of a file.")
}

// exampleCatalog registers a scripted provider so the example runs
// without network access. The script walks the flow: load a skill,
// then answer.
func exampleCatalog() *catalog.Catalog {
	c := catalog.New()
	c.RegisterProvider(&exampleProvider{responses: []*ai.EventStream{
		exampleToolCall("c1", "skill", map[string]any{"name": "commit"}),
		exampleText("Committed with an imperative subject."),
	}})
	return c
}

type exampleProvider struct {
	responses []*ai.EventStream
	callIdx   int
}

func (p *exampleProvider) ID() string { return "mock" }

func (p *exampleProvider) Models() []ai.Model {
	return []ai.Model{{ID: "small", ToolCall: true}}
}

func (p *exampleProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	if p.callIdx >= len(p.responses) {
		return exampleText("done")
	}
	resp := p.responses[p.callIdx]
	p.callIdx++
	return resp
}

func exampleText(text string) *ai.EventStream {
	return ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		push(ai.Event{Type: ai.EventTextEnd, Content: text})
		return &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    []ai.Content{ai.Text{Text: text}},
			StopReason: ai.StopReasonStop,
		}, nil
	})
}

func exampleToolCall(id, name string, args map[string]any) *ai.EventStream {
	call := ai.ToolCall{ID: id, Name: name, Arguments: args}
	return ai.NewEventStream(func(push func(ai.Event)) (*ai.Message, error) {
		push(ai.Event{Type: ai.EventToolEnd, ToolCall: &call})
		return &ai.Message{
			Role:       ai.RoleAssistant,
			Content:    []ai.Content{call},
			StopReason: ai.StopReasonToolUse,
		}, nil
	})
}

var _ = agent.WithMaxTurns // the flat option list mixes all three layers

// userConfigDir returns a directory standing in for the user's own
// config root, which a real program would read from the home directory.
func userConfigDir() string {
	dir, err := os.MkdirTemp("", "harness-user")
	if err != nil {
		panic(err)
	}
	return dir
}
