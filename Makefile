BIN     ?= build/crew
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/codyhartsook/multiplayer/internal/version.value=$(VERSION)
APP     := build/Crew.app
GOBIN_DIR := $(shell go env GOBIN)
ifeq ($(strip $(GOBIN_DIR)),)
GOBIN_DIR := $(shell go env GOPATH)/bin
endif

.PHONY: build install uninstall app install-app test fmt vet check

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

## app: build the optional macOS WebView app
app:
	@test "$$(uname -s)" = Darwin || { echo "the Crew app requires macOS"; exit 1; }
	mkdir -p $(APP)/Contents/MacOS
	cp macos/Info.plist $(APP)/Contents/Info.plist
	xcrun clang -fobjc-arc -Wall -Wextra -mmacosx-version-min=13.0 -arch arm64 -arch x86_64 \
		-framework Cocoa -framework WebKit -o $(APP)/Contents/MacOS/Crew macos/CrewApp.m
	codesign --force --sign - $(APP)

## install-app: install the app for the current user
install-app: app
	mkdir -p $(HOME)/Applications
	ditto $(APP) $(HOME)/Applications/Crew.app
	@echo "installed Crew.app to $(HOME)/Applications"

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test
	sh -n scripts/install.sh
