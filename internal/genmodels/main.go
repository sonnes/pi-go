// Command genmodels generates per-provider models.go files from the
// models.dev catalog (https://models.dev/api.json).
//
// Run it with `make gen` or `go run ./internal/genmodels`. It fetches the
// catalog and skips dated snapshots and deprecated models. For each provider
// package, it writes one var per model, plus Models() and LanguageModel().
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// target maps a models.dev provider key to a local provider package.
type target struct {
	key string // models.dev top-level provider key
	dir string // path to the provider package, relative to repo root
	pkg string // Go package name
}

var targets = []target{
	{key: "anthropic", dir: "pkg/ai/provider/anthropic", pkg: "anthropic"},
	{key: "openai", dir: "pkg/ai/provider/openai", pkg: "openai"},
	{key: "openai", dir: "pkg/ai/provider/openairesponses", pkg: "openairesponses"},
	{key: "google", dir: "pkg/ai/provider/google", pkg: "google"},
}

// mdModel is the subset of a models.dev model entry that we map to ai.Model.
type mdModel struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Reasoning        bool   `json:"reasoning"`
	ToolCall         bool   `json:"tool_call"`
	StructuredOutput bool   `json:"structured_output"`
	Temperature      bool   `json:"temperature"`
	Knowledge        string `json:"knowledge"`
	ReleaseDate      string `json:"release_date"`
	LastUpdated      string `json:"last_updated"`
	OpenWeights      bool   `json:"open_weights"`
	Status           string `json:"status"`
	Modalities       struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	ReasoningOptions []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`
	Cost struct {
		Input       float64 `json:"input"`
		Output      float64 `json:"output"`
		CacheRead   float64 `json:"cache_read"`
		CacheWrite  float64 `json:"cache_write"`
		Reasoning   float64 `json:"reasoning"`
		InputAudio  float64 `json:"input_audio"`
		OutputAudio float64 `json:"output_audio"`
	} `json:"cost"`
}

type mdProvider struct {
	Models map[string]mdModel `json:"models"`
}

const catalogURL = "https://models.dev/api.json"

func main() {
	input := flag.String("input", "", "path to a models.dev api.json (default: fetch "+catalogURL+")")
	root := flag.String("root", ".", "repo root")
	flag.Parse()

	raw, err := load(*input)
	if err != nil {
		log.Fatalf("genmodels: load catalog: %v", err)
	}

	var catalog map[string]mdProvider
	if err := json.Unmarshal(raw, &catalog); err != nil {
		log.Fatalf("genmodels: parse catalog: %v", err)
	}

	for _, t := range targets {
		prov, ok := catalog[t.key]
		if !ok {
			log.Fatalf("genmodels: provider %q not in catalog", t.key)
		}
		src, n := generate(t, prov)
		out := filepath.Join(*root, t.dir, "models.go")
		if err := os.WriteFile(out, src, 0o644); err != nil {
			log.Fatalf("genmodels: write %s: %v", out, err)
		}
		fmt.Printf("genmodels: wrote %s (%d models)\n", out, n)
	}
}

func load(path string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(catalogURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", catalogURL, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// datedID matches a snapshot suffix. A suffix is a trailing 8-digit date,
// for example "-20250805", or a "-YYYY-MM-DD" date, for example
// "-2024-08-06". The generator skips snapshot models. It emits only the
// canonical, latest IDs.
var datedID = regexp.MustCompile(`-\d{8}$|\d{4}-\d{2}-\d{2}$`)

// statusDeprecated is the models.dev marker for a model that the vendor
// retired. Such a model gives callers a spec that the API rejects. A
// deprecation upstream therefore removes the model from the catalog here.
const statusDeprecated = "deprecated"

// generate returns formatted Go source for a provider package, and the
// number of models in it. The output holds every output modality: text,
// image, audio, and video. The catalog therefore mirrors what the vendor
// offers now. generate skips dated snapshots and deprecated models.
func generate(t target, prov mdProvider) ([]byte, int) {
	models := make([]mdModel, 0, len(prov.Models))
	for _, m := range prov.Models {
		if datedID.MatchString(m.ID) {
			continue
		}
		if m.Status == statusDeprecated {
			continue
		}
		models = append(models, m)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })

	names := make([]string, len(models))
	seen := map[string]bool{}
	for i, m := range models {
		n := varName(m.ID)
		for seen[n] {
			n += "_"
		}
		seen[n] = true
		names[i] = n
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by internal/genmodels from models.dev. DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "package %s\n\n", t.pkg)
	fmt.Fprintf(&b, "import ai %q\n\n", "github.com/sonnes/pi-go/pkg/ai")
	fmt.Fprintf(&b, "// Model-info vars generated from the models.dev catalog. Each var holds\n")
	fmt.Fprintf(&b, "// pure metadata. Bind one with [Provider.LanguageModel] or\n")
	fmt.Fprintf(&b, "// [ai.NewLanguageModel].\n")
	fmt.Fprintf(&b, "var (\n")
	for i, m := range models {
		fmt.Fprintf(&b, "%s = %s\n", names[i], modelLiteral(m))
	}
	fmt.Fprintf(&b, ")\n\n")

	fmt.Fprintf(&b, "// models is the catalog that this provider serves.\n")
	fmt.Fprintf(&b, "var models = []ai.Model{\n")
	for _, n := range names {
		fmt.Fprintf(&b, "%s,\n", n)
	}
	fmt.Fprintf(&b, "}\n\n")

	fmt.Fprintf(&b, "// Models returns a copy of the models that this provider serves.\n")
	fmt.Fprintf(&b, "func Models() []ai.Model {\n")
	fmt.Fprintf(&b, "out := make([]ai.Model, len(models))\n")
	fmt.Fprintf(&b, "copy(out, models)\n")
	fmt.Fprintf(&b, "return out\n}\n\n")

	fmt.Fprintf(&b, "// LanguageModel binds a model-info value to this provider. It is a\n")
	fmt.Fprintf(&b, "// shortcut for [ai.NewLanguageModel].\n")
	fmt.Fprintf(&b, "func (p *Provider) LanguageModel(info ai.Model) ai.LanguageModel {\n")
	fmt.Fprintf(&b, "return ai.NewLanguageModel(info, p)\n}\n")

	src, err := format.Source([]byte(b.String()))
	if err != nil {
		log.Fatalf("genmodels: format %s: %v\n%s", t.pkg, err, b.String())
	}
	return src, len(models)
}

func modelLiteral(m mdModel) string {
	var f []string
	f = append(f, fmt.Sprintf("ID: %q", m.ID))
	if m.Name != "" {
		f = append(f, fmt.Sprintf("Name: %q", m.Name))
	}
	if m.Reasoning {
		f = append(f, "Reasoning: true")
	}
	if levels := thinkingLevels(m); len(levels) > 0 {
		f = append(f, "ThinkingLevels: []ai.ThinkingLevel{"+strings.Join(levels, ", ")+"}")
		f = append(f, "DefaultThinkingLevel: "+defaultThinkingLevel(levels))
	}
	if m.ToolCall {
		f = append(f, "ToolCall: true")
	}
	if m.StructuredOutput {
		f = append(f, "StructuredOutput: true")
	}
	if m.Temperature {
		f = append(f, "Temperature: true")
	}
	if in := modalities(m.Modalities.Input); in != "" {
		f = append(f, "Input: "+in)
	}
	if out := modalities(m.Modalities.Output); out != "" {
		f = append(f, "Output: "+out)
	}
	if m.Limit.Context > 0 || m.Limit.Output > 0 {
		f = append(f, fmt.Sprintf("ContextWindow: %d", m.Limit.Context))
		f = append(f, fmt.Sprintf("MaxTokens: %d", m.Limit.Output))
	}
	if cost := costLiteral(m); cost != "" {
		f = append(f, "Cost: "+cost)
	}
	if m.Knowledge != "" {
		f = append(f, fmt.Sprintf("Knowledge: %q", m.Knowledge))
	}
	if m.ReleaseDate != "" {
		f = append(f, fmt.Sprintf("ReleaseDate: %q", m.ReleaseDate))
	}
	if m.LastUpdated != "" {
		f = append(f, fmt.Sprintf("LastUpdated: %q", m.LastUpdated))
	}
	if m.OpenWeights {
		f = append(f, "OpenWeights: true")
	}
	return "ai.Model{\n" + strings.Join(f, ",\n") + ",\n}"
}

func costLiteral(m mdModel) string {
	c := m.Cost
	var f []string
	add := func(name string, v float64) {
		if v != 0 {
			f = append(f, fmt.Sprintf("%s: %g", name, v))
		}
	}
	add("Input", c.Input)
	add("Output", c.Output)
	add("CacheRead", c.CacheRead)
	add("CacheWrite", c.CacheWrite)
	add("Reasoning", c.Reasoning)
	add("InputAudio", c.InputAudio)
	add("OutputAudio", c.OutputAudio)
	if len(f) == 0 {
		return ""
	}
	return "ai.Cost{" + strings.Join(f, ", ") + "}"
}

// thinkingLevelConst maps a models.dev effort value to an ai.ThinkingLevel
// constant. It returns an empty string for a value that pi-go does not have.
func thinkingLevelConst(value string) string {
	switch value {
	case "none":
		return "ai.ThinkingOff"
	case "minimal":
		return "ai.ThinkingMinimal"
	case "low":
		return "ai.ThinkingLow"
	case "medium":
		return "ai.ThinkingMedium"
	case "high":
		return "ai.ThinkingHigh"
	case "xhigh":
		return "ai.ThinkingXHigh"
	case "max":
		return "ai.ThinkingMax"
	default:
		return ""
	}
}

// thinkingLevelRanks orders the constants that thinkingLevelConst returns. It
// mirrors the rank map in package ai.
var thinkingLevelRanks = map[string]int{
	"ai.ThinkingOff":     0,
	"ai.ThinkingMinimal": 1,
	"ai.ThinkingLow":     2,
	"ai.ThinkingMedium":  3,
	"ai.ThinkingHigh":    4,
	"ai.ThinkingXHigh":   5,
	"ai.ThinkingMax":     6,
}

// thinkingLevels returns the ai.ThinkingLevel constants for a model, in
// catalog order. Only a reasoning option of type "effort" carries levels. The
// "budget_tokens" and "toggle" options give none.
func thinkingLevels(m mdModel) []string {
	var out []string
	seen := map[string]bool{}
	for _, o := range m.ReasoningOptions {
		if o.Type != "effort" {
			continue
		}
		for _, v := range o.Values {
			c := thinkingLevelConst(v)
			if c == "" || seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// defaultThinkingLevel picks the level that a request uses when the caller
// asks for none. It prefers ai.ThinkingHigh. When a model does not offer that
// level, it returns the deepest level that the model does offer.
func defaultThinkingLevel(levels []string) string {
	best := ""
	bestRank := -1
	for _, l := range levels {
		if l == "ai.ThinkingHigh" {
			return l
		}
		if rank := thinkingLevelRanks[l]; rank > bestRank {
			best = l
			bestRank = rank
		}
	}
	return best
}

func modalities(ms []string) string {
	var out []string
	for _, m := range ms {
		if c := modalityConst(m); c != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "[]ai.Modality{" + strings.Join(out, ", ") + "}"
}

func modalityConst(m string) string {
	switch m {
	case "text":
		return "ai.ModalityText"
	case "image":
		return "ai.ModalityImage"
	case "pdf":
		return "ai.ModalityPDF"
	case "audio":
		return "ai.ModalityAudio"
	case "video":
		return "ai.ModalityVideo"
	default:
		return ""
	}
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// varName derives an exported Go identifier from a model ID. For example,
// "claude-sonnet-4-5" -> "ClaudeSonnet45" and "gpt-4o" -> "GPT4o".
func varName(id string) string {
	var b strings.Builder
	for _, p := range nonAlnum.Split(id, -1) {
		if p == "" {
			continue
		}
		if strings.EqualFold(p, "gpt") {
			b.WriteString("GPT")
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	name := b.String()
	if name == "" {
		name = "M"
	}
	if unicode.IsDigit(rune(name[0])) {
		name = "M" + name
	}
	return name
}
