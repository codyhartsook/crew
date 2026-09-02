BIN     ?= build/multiplayer
PREFIX  ?= $(HOME)/.local
BINDIR  ?= $(PREFIX)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/codyhartsook/multiplayer/internal/cli.version=$(VERSION)

.PHONY: build install uninstall test fmt vet check

## build: compile the binary to $(BIN)
build:
	mkdir -p $(dir $(BIN))
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/multiplayer

## install: install the CLI to $(BINDIR)
install: build
	install -d $(BINDIR)
	install -m 0755 $(BIN) $(BINDIR)/multiplayer

## uninstall: remove the CLI binary from $(BINDIR)
uninstall:
	rm -f $(BINDIR)/multiplayer

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test
	sh -n scripts/install.sh
