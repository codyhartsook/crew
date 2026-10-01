BIN     ?= build/crew
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/codyhartsook/multiplayer/internal/version.value=$(VERSION)
GOBIN_DIR := $(shell go env GOBIN)
ifeq ($(strip $(GOBIN_DIR)),)
GOBIN_DIR := $(shell go env GOPATH)/bin
endif

.PHONY: build install uninstall test fmt vet check

## build: compile the binary to $(BIN)
build:
	mkdir -p $(dir $(BIN))
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/crew

## install: build and install the current checkout to $(GOBIN_DIR) (go install's canonical bin dir)
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/crew
	@echo "installed $$($(GOBIN_DIR)/crew --version) to $(GOBIN_DIR)/crew"
	@case ":$$PATH:" in \
		*":$(GOBIN_DIR):"*) ;; \
		*) echo "warning: $(GOBIN_DIR) is not on PATH; add it to use crew" ;; \
	esac
	@resolved=$$(command -v crew 2>/dev/null || true); \
	if test -n "$$resolved" && test "$$resolved" != "$(GOBIN_DIR)/crew"; then \
		echo "warning: this shell resolves crew to $$resolved instead"; \
	fi

## uninstall: remove the CLI binary from $(GOBIN_DIR)
uninstall:
	rm -f $(GOBIN_DIR)/crew

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test
