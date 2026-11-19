# Development tasks. The checks are the ones CI runs (.github/workflows/ci.yml).

# List the recipes
default:
    @just --list

# Run the tests
test:
    go test ./...

# Check formatting, go.mod, vet and lint, as CI does
lint:
    #!/bin/sh
    set -eu
    unformatted=$(gofmt -l .)
    if [ -n "$unformatted" ]; then
        echo "Run gofmt -w on:"
        echo "$unformatted"
        exit 1
    fi
    go mod tidy -diff
    go vet ./...
    nix run nixpkgs#golangci-lint -- run ./...
    nix run nixpkgs#actionlint

# Format the Go code
fmt:
    gofmt -w .

# Run the tests and every check
check: lint test

# Build cg into the checkout
build:
    go build -o cg ./cmd/cg

# Build and run cg with test-config.yaml (gitignored, local only)
run *args: build
    ./cg -c test-config.yaml {{args}}

# Build the flake package, including files not yet committed (runs the tests too)
nix:
    nix build "path:$PWD"
