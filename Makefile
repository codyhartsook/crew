BIN     ?= build/crew
PREFIX  ?= $(HOME)/.local
BINDIR  ?= $(PREFIX)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/codyhartsook/multiplayer/internal/version.value=$(VERSION)

.PHONY: build install install-built uninstall test fmt vet check

## build: compile the binary to $(BIN)
build:
	mkdir -p $(dir $(BIN))
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/crew

## install: build and install the current checkout to $(BINDIR)
install: build
	$(MAKE) install-built

## install-built: install $(BIN) without rebuilding (for sudo/system prefixes)
install-built:
	@test -x $(BIN) || { \
		echo "no binary at $(BIN); run 'make build' as your own user first"; \
		exit 1; \
	}
	install -d $(BINDIR)
	install -m 0755 $(BIN) $(BINDIR)/crew
	@echo "installed $$($(BINDIR)/crew --version) to $(BINDIR)/crew"
	@resolved=$$(command -v crew 2>/dev/null || true); \
	if test -n "$$resolved" && test "$$resolved" != "$(BINDIR)/crew"; then \
		echo "warning: this shell resolves crew to $$resolved"; \
	fi

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
