BIN       := bin/lumen
PKG       := gitlab.torproject.org/cerberus-droid/lumen
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE      := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -ldflags="-s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT) -X $(PKG)/internal/version.Date=$(DATE)"

.PHONY: all build test cover vet lint race clean help

all: vet test build

build:
	@echo "==> Building $(BIN) (version=$(VERSION), commit=$(COMMIT))"
	go build -trimpath $(LDFLAGS) -o $(BIN) ./cmd/lumen/
	@echo "==> Done: $(BIN)"

test:
	@echo "==> Running tests"
	go test -count=1 ./...

race:
	@echo "==> Running tests with race detector"
	go test -race -count=1 ./...

cover:
	@echo "==> Running tests with coverage"
	mkdir -p build
	go test -count=1 -coverprofile=build/cover.out ./...
	go tool cover -html=build/cover.out -o build/cover.html
	@echo "==> Coverage report: build/cover.html"
	@go tool cover -func=build/cover.out | tail -1

vet:
	@echo "==> Running go vet"
	go vet ./...

lint:
	@echo "==> Running golangci-lint"
	golangci-lint run --timeout=5m ./...

clean:
	@echo "==> Cleaning"
	rm -rf bin/ build/
	go clean -cache -testcache

help:
	@echo "Targets:"
	@echo "  build  - Build production binary in bin/"
	@echo "  test   - Run all tests"
	@echo "  race   - Run tests with race detector"
	@echo "  cover  - Generate coverage report (build/cover.html)"
	@echo "  vet    - Run go vet"
	@echo "  lint   - Run golangci-lint"
	@echo "  clean  - Remove build artifacts"
