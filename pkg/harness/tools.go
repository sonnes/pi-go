package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/harness/def"
)

// synthTool is an [ai.Tool] the harness builds itself. It exists
// instead of [ai.DefineTool] because the skill tool's schema carries an
// enum of the resolved skill names, which a schema derived from a Go
// type cannot express.
type synthTool struct {
	info ai.ToolInfo
	run  func(ctx context.Context, call ai.ToolCallReq) (ai.ToolResult, error)
}

func (t *synthTool) Info() ai.ToolInfo { return t.info }

func (t *synthTool) Run(ctx context.Context, call ai.ToolCallReq) (ai.ToolResult, error) {
	return t.run(ctx, call)
}

// compileTools returns the tool list for an agent build: the registered
// tools, plus a "skill" tool when the build resolved skills. A harness
// with no skills exposes no skill tool.
func (b *build) compileTools(res *resolution) []ai.Tool {
	base := b.tools.list()
	tools := make([]ai.Tool, 0, len(base)+1)
	tools = append(tools, base...)
	if len(res.skills) > 0 {
		tools = append(tools, b.skillTool(res.skills))
	}
	return tools
}

// listing is one name/description pair from the artifacts, the unit a
// synthesized tool advertises itself with.
type listing struct {
	name string
	desc string
}

func skillListings(skills []def.Skill) []listing {
	out := make([]listing, len(skills))
	for i, s := range skills {
		out[i] = listing{name: s.Name, desc: scoped(s.Description, s.Scope)}
	}
	return out
}

// scoped spells out the directory a scoped artifact governs. Its name
// already carries the scope, but the description is what the model picks
// on, and two variants of one artifact are otherwise described
// identically.
func scoped(desc, scope string) string {
	if scope == "" {
		return desc
	}
	applies := fmt.Sprintf("Applies to work in %s.", scope)
	if desc == "" {
		return applies
	}
	return desc + " " + applies
}

// catalogue renders the listings as markdown bullets. The skill tool
// puts it in its description so the model can pick without a second
// lookup.
func catalogue(items []listing) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString("\n- ")
		b.WriteString(it.name)
		if it.desc != "" {
			b.WriteString(": ")
			b.WriteString(it.desc)
		}
	}
	return b.String()
}

// enumOf returns the listing names as schema enum values.
func enumOf(items []listing) []any {
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = it.name
	}
	return out
}

// skillTool synthesizes the "skill" tool. The system prompt carries
// only names and descriptions; this tool is how a body reaches the
// conversation, so a large library costs prompt tokens only for the
// skills actually used. The body it serves is the build's snapshot,
// like every other artifact.
func (b *build) skillTool(skills []def.Skill) ai.Tool {
	items := skillListings(skills)
	byName := make(map[string]def.Skill, len(skills))
	for _, s := range skills {
		byName[s.Name] = s
	}

	return &synthTool{
		info: ai.ToolInfo{
			Name: skillToolName,
			Description: "Load the instructions for a skill. Available skills:" +
				catalogue(items),
			UseWhen: "The task matches one of the available skills.",
			// A pure read: a batch that loads a skill alongside other
			// tool calls has no reason to serialize.
			Parallel: true,
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"name": {
						Type:        "string",
						Description: "The skill to load.",
						Enum:        enumOf(items),
					},
				},
				Required: []string{"name"},
			},
		},
		run: func(_ context.Context, call ai.ToolCallReq) (ai.ToolResult, error) {
			var in struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(call.Input), &in); err != nil {
				return ai.NewErrorResult(call.ID, fmt.Sprintf("invalid input: %s", err)), nil
			}

			s, ok := byName[in.Name]
			if !ok {
				return ai.NewErrorResult(call.ID, fmt.Sprintf("unknown skill %q", in.Name)), nil
			}

			body := s.Body
			if s.Dir != "" {
				body = fmt.Sprintf("Skill directory: %s\n\n%s", s.Dir, body)
			}
			return ai.NewTextResult(call.ID, body), nil
		},
	}
}
