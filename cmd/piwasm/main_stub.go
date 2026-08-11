//go:build !(js && wasm)

// Command piwasm is only meaningful as a WebAssembly module. This stub
// keeps the module able to build, vet, and test under the host
// toolchain. The demo logic in internal/demo is portable, and ordinary
// tests cover it.
package main

import "fmt"

func main() {
	fmt.Println("piwasm: build with GOOS=js GOARCH=wasm (see `make site-wasm`)")
}
