// Package cursorcli provides an [ai.TextProvider] implementation. The
// `cursor-agent` CLI does the work. The CLI runs in non-interactive
// `--print` mode.
//
// The provider is stateless. Each call starts a new subprocess. By default
// the provider uses the read-only "ask" mode of Cursor. Provider calls
// therefore behave like text generation, not like a code-editing agent.
package cursorcli
