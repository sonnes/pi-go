# Pi (Go) - Claude

## Overview

This project is a SDK for building AI agents in Go.

## Tech Stack

- Go (latest stable)

## Code Quality Expectations

### Test Driven Development

- Always write tests BEFORE writing implementation code.
- Run the failing test to confirm it fails for the right reason.
- Write the minimum implementation to make the test pass.
- Run tests again to confirm they pass.
- Only commit when tests are passing.
- Do not skip this cycle. No exceptions.
- Use github.com/stretchr/testify for assertions (require for fatal, assert for non-fatal)
- Use internal/httprr for HTTP record/replay tests against external APIs
- Table-driven tests preferred.

### Build Commands

- Use `make` as the single entry point for all build, test, check commands.
- Do not invoke `go test`, `go build`, `go vet`, `golangci-lint`, or other tools directly — use the corresponding Makefile target.
- If a new build/dev command is needed, add a Makefile target for it first.

### Go Style

Write vertical, readable Go code. Favor more lines over longer lines:

- Break long function calls with one argument per line (trailing comma on last arg)
- Use intermediate variables instead of deeply nested expressions — the compiler inlines them
- Break long conditionals into named booleans
- Use early returns to keep logic flat and reduce nesting
- Write struct literals vertically with one field per line

### Documentation

- All public functions and types should be documented with GoDoc.
- Use simple, concise, and clear language.
- Use doc.go files for detailed package-level documentation.
- Use `[pkg.Type]` annotations for relevant types in GoDoc comments.

## Makefile Targets

| Target       | Purpose                    |
| ------------ | -------------------------- |
| `make test`  | Run all tests              |
| `make check` | Run linting and formatting |

## Project Structure

## Development Rules

- Keep packages small and focused — no circular dependencies.
- Always run `make check` before committing.
