BIN     ?= $(HOME)/.local/bin/multiplayer
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/codyhartsook/multiplayer/internal/cli.version=$(VERSION)

.PHONY: build install uninstall test fmt vet check

## build: compile the binary to $(BIN)
build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/multiplayer

## install: build, then register hooks and the skill with Claude Code and Codex
install: build
	$(BIN) install

## uninstall: remove the hooks, skill and sandbox grant (keeps the store)
uninstall:
	$(BIN) uninstall --yes

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test
