# distill — task runner
#
# Run `just --list` to see all recipes. Defaults are documented in CLAUDE.md.
# Requires: go 1.26.x, golangci-lint (optional: `just lint`), just.

# Configurable knobs -----------------------------------------------------------
binary      := "distill"
pkg         := "."
eval_binary := "distill-eval"
eval_pkg    := "./cmd/distill-eval"
goflags     := ""

# Default: show available recipes.
default:
    @just --list

# Build the distill binaries into the repo root.
build:
    go build {{goflags}} -o {{binary}} {{pkg}}
    go build {{goflags}} -o {{eval_binary}} {{eval_pkg}}

# Run the distill CLI; pass extra args after `--` (e.g. `just run -- count -f README.md`).
run *ARGS:
    go run {{pkg}} {{ARGS}}

# Run the full test suite (includes integration tests that build the binary).
test:
    #!/usr/bin/env bash
    set -o pipefail
    go test ./... | tail -50

# Run only fast unit tests, skipping the binary-building integration tests.
test-short:
    go test -short ./...

# Run a single package or test by pattern (e.g. `just test-one ./internal/prompts/...`).
test-one PKG:
    go test {{PKG}}

# Race-enabled test run for concurrency-sensitive changes.
test-race:
    go test -race ./...

# Generate a coverage profile and render the HTML report.
cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out

# `go vet` across all packages.
vet:
    go vet ./...

# golangci-lint v2. Skipped gracefully if the linter is not installed.
lint:
    #!/usr/bin/env bash
    set -o pipefail
    if ! command -v golangci-lint >/dev/null 2>&1; then
        echo "golangci-lint not installed; skipping" >&2
        exit 0
    fi
    golangci-lint run ./... | tail -50

# Full local verification gate: build, vet, test, lint, mod verify.
check: build vet test lint verify
    @echo "all checks passed"

# Verify module checksums are intact.
verify:
    go mod verify

# Sync go.mod/go.sum. Mutates files; review the diff before committing.
tidy:
    go mod tidy

# Install the freshly built binaries into $(go env GOPATH)/bin (overwrites existing links).
install: build
    ln -sf "$(pwd)/{{binary}}" "$$(go env GOPATH)/bin/{{binary}}"
    ln -sf "$(pwd)/{{eval_binary}}" "$$(go env GOPATH)/bin/{{eval_binary}}"

# Remove build artifacts and coverage output.
clean:
    -rm -f {{binary}} coverage.out *.out *.test
