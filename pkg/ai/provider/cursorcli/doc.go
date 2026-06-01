// Package cursorcli provides an [ai.Provider] implementation backed by the
// `cursor-agent` CLI running in non-interactive `--print` mode.
//
// The provider is stateless: each call spawns a fresh subprocess. By default it
// uses Cursor's read-only "ask" mode so provider calls behave like text
// generation instead of code-editing agents.
package cursorcli
