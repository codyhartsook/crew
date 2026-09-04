BIN     ?= build/crew
PREFIX  ?= $(HOME)/.local
BINDIR  ?= $(PREFIX)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/codyhartsook/multiplayer/internal/version.value=$(VERSION)

.PHONY: build install uninstall test fmt vet check

## build: compile the binary to $(BIN)
build:
	mkdir -p $(dir $(BIN))
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/crew

## install: copy an already-built binary to $(BINDIR)
##
## Deliberately does not build. Installing to a system prefix needs sudo, and a
## sudo build leaves root-owned artifacts in build/ and $(BINDIR) that every
## later non-root build then fails on.
install:
	@test -x $(BIN) || { \
		echo "no binary at $(BIN); run 'make build' as your own user first"; \
		exit 1; \
	}
	install -d $(BINDIR)
	install -m 0755 $(BIN) $(BINDIR)/crew

## uninstall: remove the CLI binary from $(BINDIR)
uninstall:
	rm -f $(BINDIR)/crew

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test
	sh -n scripts/install.sh
