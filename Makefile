.PHONY: test test-pkg check tidy run build install gen \
	site-install site-dev site-build site-preview site-check \
	site-wasm site-wasm-test

# Regenerate per-provider models.go from the models.dev catalog.
gen:
	go run ./internal/genmodels

# Run tests for a single package during focused development,
# e.g. `make test-pkg PKG=./pkg/agent/...`.
test-pkg:
	go test $(PKG)

test:
	go test ./...
	go test ./pkg/agent/claude/...
	go test ./pkg/agent/codex/...
	go test ./pkg/agent/cursor/...
	go test ./pkg/ai/provider/anthropic/...
	go test ./pkg/ai/provider/claudecli/...
	go test ./pkg/ai/provider/codexcli/...
	go test ./pkg/ai/provider/cursorcli/...
	go test ./pkg/ai/provider/google/...
	go test ./pkg/ai/provider/openai/...
	go test ./pkg/ai/provider/openairesponses/...
	go test ./pkg/pi/...
	go test ./pkg/tool/find/...
	go test ./pkg/tool/grep/...
	go test ./cmd/pi/...
	go test ./cmd/piwasm/...

check:
	go vet ./...
	go vet ./pkg/agent/claude/...
	go vet ./pkg/agent/codex/...
	go vet ./pkg/agent/cursor/...
	go vet ./pkg/ai/provider/anthropic/...
	go vet ./pkg/ai/provider/claudecli/...
	go vet ./pkg/ai/provider/codexcli/...
	go vet ./pkg/ai/provider/cursorcli/...
	go vet ./pkg/ai/provider/google/...
	go vet ./pkg/ai/provider/openai/...
	go vet ./pkg/ai/provider/openairesponses/...
	go vet ./pkg/pi/...
	go vet ./pkg/tool/find/...
	go vet ./pkg/tool/grep/...
	go vet ./cmd/pi/...
	go vet ./cmd/piwasm/...
	gofmt -l .

build:
	cd cmd/pi && go build -o ../../.bin/pi .

install:
	cd cmd/pi && go install .

run:
	go run github.com/sonnes/pi-go/cmd/pi $(ARGS)

tidy:
	go mod tidy
	cd cmd/pi && go mod tidy
	cd cmd/piwasm && go mod tidy
	cd pkg/agent/claude && go mod tidy
	cd pkg/agent/codex && go mod tidy
	cd pkg/agent/cursor && go mod tidy
	cd pkg/ai/provider/anthropic && go mod tidy
	cd pkg/ai/provider/claudecli && go mod tidy
	cd pkg/ai/provider/codexcli && go mod tidy
	cd pkg/ai/provider/cursorcli && go mod tidy
	cd pkg/ai/provider/google && go mod tidy
	cd pkg/ai/provider/openai && go mod tidy
	cd pkg/ai/provider/openairesponses && go mod tidy
	cd pkg/pi && go mod tidy
	cd pkg/tool/find && go mod tidy
	cd pkg/tool/grep && go mod tidy

# ---------------------------------------------------------------------
# Site (web/): Astro + Starlight. Docs pages are read from ./docs, so a
# docs change and its site change are the same commit.
# ---------------------------------------------------------------------
site-install:
	cd web && pnpm install

site-dev:
	cd web && pnpm dev

# The demo module is a build input, not a checked-in artifact.
site-build: site-wasm
	cd web && pnpm build

site-preview:
	cd web && pnpm preview

site-check:
	cd web && pnpm check

# Build the browser demo (agent loop + session tree) to WebAssembly and
# copy the matching wasm_exec.js, so the loader can never drift from the
# toolchain that produced the module. The budget is a guard against the
# module quietly growing past what a landing page should download.
WASM_BUDGET_BYTES ?= 25000000

site-wasm:
	GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o web/public/pi-demo.wasm ./cmd/piwasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/public/wasm_exec.js
	@size=$$(wc -c < web/public/pi-demo.wasm); \
		echo "pi-demo.wasm: $$size bytes (budget $(WASM_BUDGET_BYTES))"; \
		if [ "$$size" -gt "$(WASM_BUDGET_BYTES)" ]; then \
			echo "pi-demo.wasm exceeds the budget — split the live provider out or raise WASM_BUDGET_BYTES deliberately"; \
			exit 1; \
		fi

# Drive the built module the way the page does and assert the loop ran.
site-wasm-test: site-wasm
	node web/scripts/wasm-smoke.mjs web/public/pi-demo.wasm "$$(pwd)/web/public/wasm_exec.js"
