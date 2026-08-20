.PHONY: build run install clean test test-livefire format lint
.DEFAULT_GOAL := build

# Determine the version string using git tags or fallback to 'dev'
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X github.com/bartkleypas/please/internal/engine.Version=$(VERSION)

PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

build:
	@echo "Building please version $(VERSION)..."
	go build -ldflags "$(LDFLAGS)" -o please ./cmd/please

run: build
	./please -c

install: build
	@echo "Installing please version $(VERSION) to $(BINDIR)..."
	mkdir -p $(BINDIR)
	install -m 755 please $(BINDIR)/please

test:
	go test ./...

test-livefire:
	PLEASE_LIVE_FIRE=1 go test -v ./internal/engine

format:
	go fmt ./...

lint:
	go vet ./...

clean:
	rm -f please
