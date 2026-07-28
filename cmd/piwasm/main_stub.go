//go:build !(js && wasm)

// Command piwasm is only meaningful as a WebAssembly module. This stub
// exists so the module still builds, vets, and tests under the host
// toolchain — the demo logic in internal/demo is portable and covered
// by ordinary tests.
package main

import "fmt"

func main() {
	fmt.Println("piwasm: build with GOOS=js GOARCH=wasm (see `make site-wasm`)")
}
