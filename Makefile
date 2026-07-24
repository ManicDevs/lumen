# =============================================================================
# Lumen — LLM Code Diagnostic Engine
# =============================================================================
# Usage:  make help          — show all available targets
#         make all           — vet + lint + test + build (default)
#         make quick         — fast feedback: vet + test + build
#         make ci            — full CI pipeline

SHELL := /bin/bash
.DEFAULT_GOAL := all

# ── Project metadata ──────────────────────────────────────────────────────────
PKG       := gitlab.torproject.org/cerberus-droid/lumen
BIN       := bin/lumen
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE      := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOVERSION := $(shell go version | awk '{print $$3}')

# ── Build flags ───────────────────────────────────────────────────────────────
LDFLAGS   := -s -w \
             -X $(PKG)/internal/version.Version=$(VERSION) \
             -X $(PKG)/internal/version.Commit=$(COMMIT) \
             -X $(PKG)/internal/version.Date=$(DATE)
GOFLAGS   := -trimpath
GOTEST    := go test
GOTEST_FLAGS := -count=1

# ── Directories ───────────────────────────────────────────────────────────────
COVERDIR  := build/coverage
BENCHDIR  := build/bench

# ── Go version for compat check ──────────────────────────────────────────────
MIN_GO    := 1.21

# =============================================================================
# TARGETS
# =============================================================================

.PHONY: all quick ci
.PHONY: build build-release install uninstall
.PHONY: test test-verbose test-short test-package test-race test-3x
.PHONY: bench bench-mem bench-compare
.PHONY: fuzz fuzz-corpus
.PHONY: cover cover-html cover-func cover-xml cover-open cover-clean
.PHONY: vet lint fmt fmt-check imports staticcheck
.PHONY: check verify tidy download
.PHONY: clean clean-all
.PHONY: docker docker-push
.PHONY: release release-snapshot
.PHONY: docs godoc
.PHONY: help env info version

# ──────────────────────────────────────────────────────────────────────────────
# Pipeline targets
# ──────────────────────────────────────────────────────────────────────────────

all: lint test build
	@echo "==> All checks passed."

quick: vet test build
	@echo "==> Quick build complete."

ci: check test-race lint build
	@echo "==> CI pipeline passed."

check: fmt-check vet lint
	@echo "==> Static checks passed."

# ──────────────────────────────────────────────────────────────────────────────
# Build
# ──────────────────────────────────────────────────────────────────────────────

build:
	@echo "==> Building $(BIN) $(VERSION) ($(COMMIT))"
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/lumen/
	@echo "==> Binary: $(BIN)"

build-release:
	@echo "==> Building release binary (stripped, static)"
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/lumen/
	@echo "==> Binary: $(BIN)"

install:
	@echo "==> Installing to $(GOPATH)/bin/lumen"
	go install $(GOFLAGS) -ldflags "$(LDFLAGS)" ./cmd/lumen/

uninstall:
	@echo "==> Removing $(GOPATH)/bin/lumen"
	rm -f $(GOPATH)/bin/lumen

# ──────────────────────────────────────────────────────────────────────────────
# Test
# ──────────────────────────────────────────────────────────────────────────────

test:
	@echo "==> Running tests"
	$(GOTEST) $(GOTEST_FLAGS) ./...

test-verbose:
	@echo "==> Running tests (verbose)"
	$(GOTEST) $(GOTEST_FLAGS) -v ./...

test-short:
	@echo "==> Running tests (short mode)"
	$(GOTEST) $(GOTEST_FLAGS) -short ./...

test-package:
	@echo "==> Running tests for package: $(PKG)"
	$(GOTEST) $(GOTEST_FLAGS) -v -run $(PKG) ./...

test-race:
	@echo "==> Running tests with race detector"
	$(GOTEST) $(GOTEST_FLAGS) -race ./...

test-3x:
	@echo "==> Running tests 3x with race detector (stress test)"
	$(GOTEST) -count=3 -race -timeout 180s ./...

test-bench:
	@echo "==> Running tests with benchmarks"
	$(GOTEST) $(GOTEST_FLAGS) -bench=. -benchmem ./...

# ──────────────────────────────────────────────────────────────────────────────
# Benchmarks
# ──────────────────────────────────────────────────────────────────────────────

bench:
	@echo "==> Running benchmarks"
	$(GOTEST) -bench=. -benchmem -count=3 ./... | tee $(BENCHDIR)/bench.txt

bench-mem:
	@echo "==> Running benchmarks (memory profile)"
	mkdir -p $(BENCHDIR)
	$(GOTEST) -bench=. -benchmem -memprofile=$(BENCHDIR)/mem.out ./...
	go tool pprof -alloc_space -top $(BENCHDIR)/mem.out

bench-compare:
	@echo "==> Comparing benchmark results"
	@which benchstat > /dev/null 2>&1 || (echo "Install benchstat: go install golang.org/x/perf/cmd/benchstat@latest" && exit 1)
	benchstat $(BENCHDIR)/bench.txt

# ──────────────────────────────────────────────────────────────────────────────
# Fuzz testing
# ──────────────────────────────────────────────────────────────────────────────

fuzz:
	@echo "==> Fuzzing parser (60s)"
	$(GOTEST) -fuzz=FuzzParseFileBlocks -fuzztime=60s ./internal/agent/ || true
	@echo "==> Fuzzing dotenv (60s)"
	$(GOTEST) -fuzz=FuzzLoadDotenv -fuzztime=60s ./internal/env/ || true
	@echo "==> Fuzzing redact (60s)"
	$(GOTEST) -fuzz=FuzzRedact -fuzztime=60s ./internal/harvest/ || true

fuzz-corpus:
	@echo "==> Collecting fuzz corpus"
	find . -path "*/testdata/fuzz/*" -type f | head -20

# ──────────────────────────────────────────────────────────────────────────────
# Coverage
# ──────────────────────────────────────────────────────────────────────────────

cover:
	@echo "==> Running tests with coverage"
	mkdir -p $(COVERDIR)
	$(GOTEST) $(GOTEST_FLAGS) -coverprofile=$(COVERDIR)/cover.out ./...
	@echo ""
	@echo "── Summary ────────────────────────────────────────────────"
	@go tool cover -func=$(COVERDIR)/cover.out | tail -1
	@echo "────────────────────────────────────────────────────────────"

cover-html: cover
	@echo "==> Generating HTML coverage report"
	go tool cover -html=$(COVERDIR)/cover.out -o $(COVERDIR)/cover.html
	@echo "==> Report: $(COVERDIR)/cover.html"

cover-func: cover
	@echo "==> Per-function coverage"
	@go tool cover -func=$(COVERDIR)/cover.out

cover-xml: cover
	@echo "==> Generating XML coverage report"
	@which gocov-xml > /dev/null 2>&1 || (echo "Install gocov-xml: go install github.com/boumenot/gocov-xml@latest" && exit 1)
	@which gocov > /dev/null 2>&1 || (echo "Install gocov: go install github.com/axw/gocov@latest" && exit 1)
	gocov convert $(COVERDIR)/cover.out | gocov-xml > $(COVERDIR)/coverage.xml
	@echo "==> Report: $(COVERDIR)/coverage.xml"

cover-open: cover-html
	@echo "==> Opening coverage report"
	@which xdg-open > /dev/null 2>&1 && xdg-open $(COVERDIR)/cover.html || \
	@which open > /dev/null 2>&1 && open $(COVERDIR)/cover.html || \
	echo "==> Open $(COVERDIR)/cover.html manually"

cover-clean:
	rm -rf $(COVERDIR)

# ──────────────────────────────────────────────────────────────────────────────
# Static analysis
# ──────────────────────────────────────────────────────────────────────────────

vet:
	@echo "==> Running go vet"
	go vet ./...

lint:
	@echo "==> Running golangci-lint"
	@which golangci-lint > /dev/null 2>&1 || \
		(echo "Installing golangci-lint..." && \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run --timeout=5m ./...

fmt:
	@echo "==> Formatting code"
	gofmt -s -w .
	@which goimports > /dev/null 2>&1 && goimports -w -local $(PKG) . || true

fmt-check:
	@echo "==> Checking formatting"
	@DIFF=$$(gofmt -s -d .); \
	if [ -n "$$DIFF" ]; then \
		echo "$$DIFF"; \
		echo "==> Run 'make fmt' to fix formatting"; \
		exit 1; \
	fi
	@echo "==> Formatting OK"

imports:
	@echo "==> Fixing imports"
	@which goimports > /dev/null 2>&1 || (echo "Install: go install golang.org/x/tools/cmd/goimports@latest" && exit 1)
	goimports -w -local $(PKG) .

staticcheck:
	@echo "==> Running staticcheck"
	@which staticcheck > /dev/null 2>&1 || (echo "Install: go install honnef.co/go/tools/cmd/staticcheck@latest" && exit 1)
	staticcheck ./...

# ──────────────────────────────────────────────────────────────────────────────
# Dependency management
# ──────────────────────────────────────────────────────────────────────────────

tidy:
	@echo "==> Tidying modules"
	go mod tidy
	@echo "==> Verifying modules"
	go mod verify

download:
	@echo "==> Downloading dependencies"
	go mod download

deps:
	@echo "==> Module graph"
	go mod graph

deps-verify: tidy
	@echo "==> Verifying dependency checksums"
	go mod verify
	@echo "==> Checking for unused dependencies"
	go mod tidy
	@git diff --exit-code go.mod go.sum || (echo "==> go.mod/go.sum dirty after tidy" && exit 1)

# ──────────────────────────────────────────────────────────────────────────────
# Security
# ──────────────────────────────────────────────────────────────────────────────

audit:
	@echo "==> Running govulncheck"
	@which govulncheck > /dev/null 2>&1 || (echo "Install: go install golang.org/x/vuln/cmd/govulncheck@latest" && exit 1)
	govulncheck ./...

# ──────────────────────────────────────────────────────────────────────────────
# Docker
# ──────────────────────────────────────────────────────────────────────────────

docker:
	@echo "==> Building Docker image"
	docker build -t lumen:$(VERSION) -t lumen:latest .

docker-push: docker
	@echo "==> Pushing Docker image"
	docker push lumen:$(VERSION)
	docker push lumen:latest

# ──────────────────────────────────────────────────────────────────────────────
# Release
# ──────────────────────────────────────────────────────────────────────────────

release: clean ci build-release
	@echo "==> Release binary ready: $(BIN)"
	@ls -lh $(BIN)

release-snapshot: clean
	@echo "==> Building snapshot release"
	@which goreleaser > /dev/null 2>&1 || (echo "Install: go install github.com/goreleaser/goreleaser@latest" && exit 1)
	goreleaser release --snapshot --clean

# ──────────────────────────────────────────────────────────────────────────────
# Documentation
# ──────────────────────────────────────────────────────────────────────────────

docs:
	@echo "==> Generating documentation"
	mkdir -p build/docs
	@for pkg in $$(go list ./...); do \
		name=$$(echo $$pkg | sed 's|$(PKG)/||' | tr '/' '-'); \
		go doc -all $$pkg > build/docs/$$name.txt 2>/dev/null || true; \
	done
	@echo "==> Documentation: build/docs/"

godoc:
	@echo "==> Starting local godoc server"
	@which godoc > /dev/null 2>&1 || (echo "Install: go install golang.org/x/tools/cmd/godoc@latest" && exit 1)
	godoc -http=:6060 &
	@echo "==> http://localhost:6060/pkg/$(PKG)"

# ──────────────────────────────────────────────────────────────────────────────
# Cleanup
# ──────────────────────────────────────────────────────────────────────────────

clean:
	@echo "==> Cleaning build artifacts"
	rm -rf bin/ build/

clean-all: clean
	@echo "==> Cleaning Go caches"
	go clean -cache -testcache
	rm -rf $(COVERDIR) $(BENCHDIR)

# ──────────────────────────────────────────────────────────────────────────────
# Info / Debug
# ──────────────────────────────────────────────────────────────────────────────

env:
	@echo "BIN=$(BIN)"
	@echo "PKG=$(PKG)"
	@echo "VERSION=$(VERSION)"
	@echo "COMMIT=$(COMMIT)"
	@echo "DATE=$(DATE)"
	@echo "GOVERSION=$(GOVERSION)"
	@echo "LDFLAGS=$(LDFLAGS)"

info: env
	@echo ""
	@echo "── Go environment ─────────────────────────────────────────"
	@go env GOOS GOARCH GOPATH GOMODCACHE
	@echo ""
	@echo "── Module ────────────────────────────────────────────────"
	@go list -m all 2>/dev/null | head -20
	@echo ""
	@echo "── Packages ──────────────────────────────────────────────"
	@go list ./... | wc -l
	@echo "packages total"

version:
	@$(BIN) --version 2>/dev/null || echo "Binary not built yet"

# ──────────────────────────────────────────────────────────────────────────────
# Help (self-documenting)
# ──────────────────────────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "  Lumen Makefile — LLM Code Diagnostic Engine"
	@echo "  ─────────────────────────────────────────────────────────"
	@echo ""
	@echo "  Pipelines:"
	@echo "    all            Run vet + lint + test + build (default)"
	@echo "    quick          Fast feedback: vet + test + build"
	@echo "    ci             Full CI: check + test-race + lint + build"
	@echo "    check          Static checks: fmt-check + vet + lint"
	@echo ""
	@echo "  Build:"
	@echo "    build          Build binary to bin/"
	@echo "    build-release  Build stripped/static release binary"
	@echo "    install        Install to $$GOPATH/bin"
	@echo "    uninstall      Remove from $$GOPATH/bin"
	@echo ""
	@echo "  Test:"
	@echo "    test           Run all tests"
	@echo "    test-verbose   Run tests with verbose output"
	@echo "    test-short     Run tests in short mode"
	@echo "    test-race      Run tests with race detector"
	@echo "    test-3x        Stress test: 3x with race detector"
	@echo "    test-bench     Run tests with benchmarks"
	@echo ""
	@echo "  Benchmarks:"
	@echo "    bench          Run benchmarks (save results)"
	@echo "    bench-mem      Run benchmarks with memory profile"
	@echo "    bench-compare  Compare benchmark results"
	@echo ""
	@echo "  Fuzz:"
	@echo "    fuzz           Run fuzz tests for 60s each"
	@echo "    fuzz-corpus    List fuzz corpus files"
	@echo ""
	@echo "  Coverage:"
	@echo "    cover          Generate coverage report"
	@echo "    cover-html     Generate HTML coverage report"
	@echo "    cover-func     Show per-function coverage"
	@echo "    cover-xml      Generate XML coverage report"
	@echo "    cover-open     Open HTML report in browser"
	@echo "    cover-clean    Remove coverage artifacts"
	@echo ""
	@echo "  Analysis:"
	@echo "    vet            Run go vet"
	@echo "    lint           Run golangci-lint"
	@echo "    fmt            Format code (gofmt + goimports)"
	@echo "    fmt-check      Check formatting without modifying"
	@echo "    imports        Fix import ordering"
	@echo "    staticcheck    Run staticcheck"
	@echo "    audit          Run govulncheck (security)"
	@echo ""
	@echo "  Dependencies:"
	@echo "    tidy           Tidy and verify modules"
	@echo "    download       Download module dependencies"
	@echo "    deps           Show module dependency graph"
	@echo "    deps-verify    Verify deps + check for unused"
	@echo ""
	@echo "  Release:"
	@echo "    release        Clean + CI + build release binary"
	@echo "    release-snapshot  Build snapshot with goreleaser"
	@echo ""
	@echo "  Docker:"
	@echo "    docker         Build Docker image"
	@echo "    docker-push    Push Docker image"
	@echo ""
	@echo "  Documentation:"
	@echo "    docs           Generate pkg docs to build/docs/"
	@echo "    godoc          Start local godoc server"
	@echo ""
	@echo "  Utilities:"
	@echo "    clean          Remove bin/ and build/"
	@echo "    clean-all      Clean + Go caches"
	@echo "    env            Show build variables"
	@echo "    info           Show project and Go environment"
	@echo "    version        Run built binary --version"
	@echo "    help           Show this help"
	@echo ""
