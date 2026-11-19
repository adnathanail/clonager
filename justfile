# Development tasks. CI (.github/workflows/ci.yml) runs these recipes too,
# so a check passes locally exactly when it passes there.

# List the recipes
default:
    @just --list

# Run the tests, passing any arguments to go test (CI adds -race)
test *args:
    go test {{args}} ./...

# Run every check CI's lint job does, and actionlint
lint: fmt-check tidy-check vet golangci actionlint

# Fail if any Go file isn't gofmt-formatted
fmt-check:
    #!/bin/sh
    unformatted=$(gofmt -l .)
    if [ -n "$unformatted" ]; then
        echo "Run just fmt, or gofmt -w on:"
        echo "$unformatted"
        exit 1
    fi

# Fail if go.mod or go.sum need go mod tidy
tidy-check:
    go mod tidy -diff

# Run go vet
vet:
    go vet ./...

# Run golangci-lint (CI runs it with golangci-lint-action instead, for its
# annotations and cache)
golangci:
    nix run nixpkgs#golangci-lint -- run ./...

# Lint the GitHub workflows
actionlint:
    nix run nixpkgs#actionlint

# Format the Go code
fmt:
    gofmt -w .

# Run every check, then the tests
check: lint test

# Build cg into the checkout (CI sets CGO_ENABLED=0, as for the release binary)
build:
    go build -o cg ./cmd/cg

# Build and run cg with test-config.yaml (gitignored, local only)
run *args: build
    ./cg -c test-config.yaml {{args}}

# Build the flake package, including files not yet committed (runs the tests
# too), passing any arguments to nix build
nix *args:
    nix build "path:$PWD" {{args}}
