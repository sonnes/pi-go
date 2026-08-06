package fs_test

import (
	"context"
	"embed"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sonnes/pi-go/pkg/agent"
	"github.com/sonnes/pi-go/pkg/ai"
	"github.com/sonnes/pi-go/pkg/catalog"
	"github.com/sonnes/pi-go/pkg/harness"
	"github.com/sonnes/pi-go/pkg/harness/prompt"
)

// mockProvider is enough of a provider for "mock/small" to resolve.
// These tests build agents to inspect the prompt.Env the harness
// assembles; none of them run a turn.
type mockProvider struct{}

func (m *mockProvider) ID() string { return "mock" }

func (m *mockProvider) Models() []ai.Model {
	return []ai.Model{{ID: "small", ToolCall: true}}
}

func (m *mockProvider) StreamText(
	_ context.Context,
	_ ai.Model,
	_ ai.Prompt,
	_ ai.StreamOptions,
) *ai.EventStream {
	return ai.NewEventStream(func(func(ai.Event)) (*ai.Message, error) {
		return nil, errors.New("mock: these tests never run a turn")
	})
}

// testCatalog registers the mock provider so "mock/small" resolves.
func testCatalog(p *mockProvider) *catalog.Catalog {
	c := catalog.New()
	c.RegisterProvider(p)
	return c
}

// captureEnv is a prompt builder that records the Env it is handed.
func captureEnv(into **prompt.Env) prompt.Builder {
	return func(_ context.Context, env *prompt.Env) (string, error) {
		*into = env
		return "SYSTEM", nil
	}
}

// envFor builds a one-off harness with opts and returns the prompt.Env
// its builder was handed.
func envFor(t *testing.T, opts ...agent.Option) *prompt.Env {
	t.Helper()
	var captured *prompt.Env

	base := []agent.Option{
		harness.WithCatalog(testCatalog(&mockProvider{})),
		harness.WithDefaultModel("mock/small"),
		harness.WithPromptBuilder(captureEnv(&captured)),
	}
	h, err := harness.New(append(base, opts...)...)
	require.NoError(t, err)

	_, err = h.Agent(context.Background())
	require.NoError(t, err)
	require.NotNil(t, captured)
	return captured
}

// unreadable wraps an FS so one path fails to open with a non-ErrNotExist
// error, standing in for a permissions or I/O failure that no amount of
// good authoring can fix.
type unreadable struct {
	iofs.FS
	path string
	err  error
}

func (u unreadable) Open(name string) (iofs.File, error) {
	if name == u.path {
		return nil, u.err
	}
	return u.FS.Open(name)
}

// writeTree writes files (path → content) under a fresh temp dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

// The `all:` prefix is required: without it, embed skips the dotted
// .agents directory the convention is built on.
//
//go:embed all:testdata
var builtin embed.FS
