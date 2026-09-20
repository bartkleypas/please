.PHONY: build run install clean test test-canary seed-vault test-e2e test-livefire format lint
.DEFAULT_GOAL := build

# Determine the version string using git tags or fallback to 'dev'
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X github.com/bartkleypas/please/internal/engine.Version=$(VERSION)

PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

build:
	@echo "Building please version $(VERSION)..."
	go build -ldflags "$(LDFLAGS)" -o please ./cmd/please
	ln -sf please please-acp

run: build
	./please

install: build
	@echo "Installing please version $(VERSION) to $(BINDIR)..."
	mkdir -p $(BINDIR)
	install -m 755 please $(BINDIR)/please
	ln -sf please $(BINDIR)/please-acp

test:
	go test ./...

test-canary:
	PLEASE_E2E=1 go test -v -tags=e2e -timeout 10m ./test/e2e/...

seed-vault:
	go run ./test/scenarios/seeder -vault ./test_vault/e2e.db

test-e2e: test-canary
	PLEASE_E2E=1 go test -v -tags=e2e -timeout 30m ./test/scenarios/... ./cmd/please

test-livefire: test-e2e

format:
	go fmt ./...

lint:
	go vet ./...

clean:
	rm -f please please-acp
