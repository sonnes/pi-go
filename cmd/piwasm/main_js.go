//go:build js && wasm

// Command piwasm runs the pi-go agent loop, its typed tools, and its
// session tree inside a browser, for the demo on the project site.
//
// It is not the pi CLI: a browser has no subprocesses, so the
// subprocess-backed agents cannot run here. What it does show is the
// part that is pure Go — the loop, tool dispatch, and the durable
// session tree — against a scripted provider, or against OpenRouter
// with a key the visitor supplies.
package main

func main() {
	b := &bridge{}
	b.install()

	// Keep the module alive so the page can call into it.
	select {}
}
