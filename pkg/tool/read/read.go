// Package read provides the Read tool for reading files.
package read

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sonnes/pi-go/pkg/ai"

	_ "embed"
)

// Description is the default tool documentation, embedded from
// description.md. Clients register the tool under any name and may pass
// this (or their own text) as the description.
//
//go:embed description.md
var Description string

// ToolName is the suggested default registry name for this tool.
const ToolName = "read"

const (
	// DefaultLimit is the default number of lines to read.
	DefaultLimit = 2000
	// MaxLineLength is the maximum length of a line before truncation.
	MaxLineLength = 2000
)

// Input defines the parameters for the Read tool.
type Input struct {
	Offset *int   `json:"offset,omitempty" jsonschema:"1-based line number to start reading from. Omit to start at the first line."`
	Limit  *int   `json:"limit,omitempty" jsonschema:"Maximum number of lines to read. Omit for the default cap."`
	Path   string `json:"path" jsonschema:"Path to the file to read. Absolute, or relative to the working directory."`
}

// fsys is the filesystem the Read tool reads from: it opens files and
// resolves user paths against its own root, so no separate working
// directory is needed.
type fsys interface {
	fs.FS
	Resolve(path string) (string, error)
}

// reader reads files from a confined filesystem. It carries only the
// tool's real dependency — identity (name, description) belongs to the
// client that registers it.
type reader struct{ fsys fsys }

// Option configures a Read tool.
type Option interface{ apply(*reader) }

// FS sets the filesystem the tool reads from. It resolves and confines
// paths against its own root (e.g. a sandbox.FS).
type FS struct{ FS fsys }

func (f FS) apply(r *reader) { r.fsys = f.FS }

// New returns the Read tool's runner: it reads a file and returns the
// contents (or a media result for images and PDFs). Pass it to
// [ai.DefineTool]/[ai.DefineParallelTool] with a name and description.
func New(opts ...Option) func(context.Context, Input) (ai.ToolResult, error) {
	r := &reader{}
	for _, o := range opts {
		o.apply(r)
	}
	return r.run
}

func (r *reader) run(_ context.Context, input Input) (ai.ToolResult, error) {
	name, err := r.fsys.Resolve(input.Path)
	if err != nil {
		return ai.ToolResult{}, err
	}

	file, err := r.fsys.Open(name)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close() //nolint:errcheck

	info, err := file.Stat()
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return ai.ToolResult{}, fmt.Errorf("cannot read directory, use ls command instead: %s", name)
	}

	offset := 0
	if input.Offset != nil {
		offset = *input.Offset - 1
	}

	limit := DefaultLimit
	if input.Limit != nil {
		limit = *input.Limit
	}

	if mediaType, ok := imageMediaType(name); ok {
		data, readErr := io.ReadAll(file)
		if readErr != nil {
			return ai.ToolResult{}, fmt.Errorf("failed to read image: %w", readErr)
		}
		return ai.ToolResult{Type: "image", Data: data, MediaType: mediaType}, nil
	}

	if strings.EqualFold(filepath.Ext(name), ".ipynb") {
		data, readErr := io.ReadAll(file)
		if readErr != nil {
			return ai.ToolResult{}, fmt.Errorf("failed to read notebook: %w", readErr)
		}
		rendered, renderErr := formatNotebook(data)
		if renderErr != nil {
			return ai.ToolResult{}, renderErr
		}
		content, formatErr := Format(strings.NewReader(rendered), offset, limit)
		if formatErr != nil {
			return ai.ToolResult{}, formatErr
		}
		return ai.ToolResult{Type: "text", Content: content}, nil
	}

	if strings.EqualFold(filepath.Ext(name), ".pdf") {
		data, readErr := io.ReadAll(file)
		if readErr != nil {
			return ai.ToolResult{}, fmt.Errorf("failed to read PDF: %w", readErr)
		}
		content, formatErr := Format(strings.NewReader(extractPDFText(data)), offset, limit)
		if formatErr != nil {
			return ai.ToolResult{}, formatErr
		}
		return ai.ToolResult{
			Type:      "media",
			Content:   content,
			Data:      data,
			MediaType: "application/pdf",
		}, nil
	}

	content, err := Format(file, offset, limit)
	if err != nil {
		return ai.ToolResult{}, err
	}
	return ai.ToolResult{Type: "text", Content: content}, nil
}

func imageMediaType(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg":
		return "image/jpeg", true
	case ".png", ".jpeg", ".gif", ".webp":
		if mediaType := mime.TypeByExtension(ext); mediaType != "" {
			return strings.Split(mediaType, ";")[0], true
		}
	}
	return "", false
}

type notebookDoc struct {
	Cells []notebookCell `json:"cells"`
}

type notebookCell struct {
	CellType string           `json:"cell_type"`
	Source   json.RawMessage  `json:"source"`
	Outputs  []notebookOutput `json:"outputs"`
}

type notebookOutput struct {
	Data       map[string]json.RawMessage `json:"data"`
	OutputType string                     `json:"output_type"`
	Name       string                     `json:"name"`
	Text       json.RawMessage            `json:"text"`
}

func formatNotebook(data []byte) (string, error) {
	var doc notebookDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("failed to parse notebook: %w", err)
	}

	var b strings.Builder
	for i, cell := range doc.Cells {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "Cell %d [%s]\n", i+1, cell.CellType)
		b.WriteString(rawNotebookText(cell.Source))

		for _, output := range cell.Outputs {
			text := rawNotebookText(output.Text)
			if text == "" && output.Data != nil {
				text = rawNotebookText(output.Data["text/plain"])
			}
			if text == "" {
				continue
			}
			label := output.OutputType
			if output.Name != "" {
				label = output.Name
			}
			fmt.Fprintf(&b, "\n\nOutput [%s]\n%s", label, text)
		}
	}
	return b.String(), nil
}

func rawNotebookText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var parts []string
	if err := json.Unmarshal(raw, &parts); err == nil {
		return strings.Join(parts, "")
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	return ""
}

var pdfLiteralRE = regexp.MustCompile(`\(([^()]*)\)`)

func extractPDFText(data []byte) string {
	matches := pdfLiteralRE.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return "(no extractable PDF text)"
	}

	lines := make([]string, 0, len(matches))
	for _, match := range matches {
		lines = append(lines, unescapePDFLiteral(string(match[1])))
	}
	return strings.Join(lines, "\n")
}

func unescapePDFLiteral(s string) string {
	replacer := strings.NewReplacer(
		`\\`, `\`,
		`\(`, `(`,
		`\)`, `)`,
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
	)
	return replacer.Replace(s)
}

// Format renders the contents of r the same way the Read tool's on-disk
// path does — six-wide right-justified line numbers, tab, then the line,
// with [MaxLineLength] per-line truncation and the "(empty file)"
// sentinel. Callers that already have a buffer in memory (e.g. attachment
// renderers) can pass `strings.NewReader(content)` to render it as if
// Read had been called on the path.
//
// offset is zero-based and skips that many leading lines; limit caps the
// number of emitted lines. Non-positive offset/limit are normalized to
// "no skip" / [DefaultLimit].
func Format(r io.Reader, offset, limit int) (string, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = DefaultLimit
	}

	scanner := bufio.NewScanner(r)
	var lines []string
	lineNum := 0

	for scanner.Scan() {
		lineNum++

		if lineNum <= offset {
			continue
		}

		if len(lines) >= limit {
			break
		}

		line := scanner.Text()

		if len(line) > MaxLineLength {
			line = line[:MaxLineLength] + "..."
		}

		lines = append(lines, fmt.Sprintf("%6d\t%s", lineNum, line))
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}

	if len(lines) == 0 {
		return "(empty file)", nil
	}

	return strings.Join(lines, "\n"), nil
}
