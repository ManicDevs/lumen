# =============================================================================
# Lumen — LLM Code Diagnostic Engine
# =============================================================================
# Usage:  make help          — show all available targets
#         make all           — vet + lint + test + build (default)
#         make quick         — fast feedback: vet + test + build
#         make ci            — full CI pipeline
#         make setup         — install hooks + tools

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
PROFDIR   := build/profiles

# ── Platforms for cross-compilation ───────────────────────────────────────────
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# =============================================================================
# TARGETS
# =============================================================================

.PHONY: all quick ci check
.PHONY: build build-release build-all install uninstall
.PHONY: test test-verbose test-short test-race test-3x test-bench test-integration
.PHONY: bench bench-mem bench-cpu bench-compare
.PHONY: fuzz fuzz-corpus
.PHONY: cover cover-html cover-func cover-xml cover-open cover-clean cover-badge
.PHONY: profile profile-cpu profile-mem profile-block profile-trace
.PHONY: vet lint fmt fmt-check imports staticcheck
.PHONY: check verify tidy download deps deps-verify
.PHONY: audit vuln
.PHONY: clean clean-all
.PHONY: docker docker-run docker-shell docker-push
.PHONY: release release-snapshot tag
.PHONY: docs godoc
.PHONY: setup hooks-install hooks-uninstall
.PHONY: generate stringer
.PHONY: metrics loc complexity
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

check: fmt-check vet lint audit
	@echo "==> Static checks passed."

# ──────────────────────────────────────────────────────────────────────────────
# Setup
# ──────────────────────────────────────────────────────────────────────────────

setup: hooks-install
	@echo "==> Installing development tools..."
	@which golangci-lint > /dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@which govulncheck > /dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	@which goimports > /dev/null 2>&1 || go install golang.org/x/tools/cmd/goimports@latest
	@which benchstat > /dev/null 2>&1 || go install golang.org/x/perf/cmd/benchstat@latest
	@echo "==> Setup complete."

hooks-install:
	@echo "==> Installing git hooks"
	@mkdir -p .git/hooks
	@cp .githooks/pre-commit .git/hooks/pre-commit
	@cp .githooks/pre-push .git/hooks/pre-push
	@cp .githooks/commit-msg .git/hooks/commit-msg
	@chmod +x .git/hooks/pre-commit .git/hooks/pre-push .git/hooks/commit-msg
	@echo "==> Git hooks installed."

hooks-uninstall:
	@echo "==> Removing git hooks"
	@rm -f .git/hooks/pre-commit .git/hooks/pre-push .git/hooks/commit-msg
	@echo "==> Git hooks removed."

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

build-all:
	@echo "==> Building for all platforms"
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=""; if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
			-o dist/lumen-$$os-$$arch$$ext ./cmd/lumen/; \
	done
	@echo "==> Binaries in dist/"
	@ls -lh dist/

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

test-race:
	@echo "==> Running tests with race detector"
	$(GOTEST) $(GOTEST_FLAGS) -race ./...

test-3x:
	@echo "==> Running tests 3x with race detector (stress test)"
	$(GOTEST) -count=3 -race -timeout 180s ./...

test-bench:
	@echo "==> Running tests with benchmarks"
	$(GOTEST) $(GOTEST_FLAGS) -bench=. -benchmem ./...

test-integration:
	@echo "==> Running integration tests (requires Ollama)"
	$(GOTEST) $(GOTEST_FLAGS) -tags=integration -v ./...

# ──────────────────────────────────────────────────────────────────────────────
# Benchmarks
# ──────────────────────────────────────────────────────────────────────────────

bench:
	@echo "==> Running benchmarks"
	@mkdir -p $(BENCHDIR)
	$(GOTEST) -bench=. -benchmem -count=5 ./... | tee $(BENCHDIR)/bench.txt
	@echo "==> Results saved to $(BENCHDIR)/bench.txt"

bench-mem:
	@echo "==> Running benchmarks (memory profile)"
	@mkdir -p $(BENCHDIR)
	$(GOTEST) -bench=. -benchmem -memprofile=$(BENCHDIR)/mem.out ./...
	go tool pprof -alloc_space -top $(BENCHDIR)/mem.out

bench-cpu:
	@echo "==> Running benchmarks (CPU profile)"
	@mkdir -p $(BENCHDIR)
	$(GOTEST) -bench=. -cpuprofile=$(BENCHDIR)/cpu.out ./...
	go tool pprof -top $(BENCHDIR)/cpu.out

bench-compare:
	@echo "==> Comparing benchmark results"
	@which benchstat > /dev/null 2>&1 || (echo "Install benchstat: go install golang.org/x/perf/cmd/benchstat@latest" && exit 1)
	benchstat $(BENCHDIR)/bench.txt

# ──────────────────────────────────────────────────────────────────────────────
# Profiling
# ──────────────────────────────────────────────────────────────────────────────

profile: profile-cpu

profile-cpu:
	@echo "==> CPU profile: 30s (http://localhost:6060/debug/pprof/)"
	@mkdir -p $(PROFDIR)
	go test -cpuprofile=$(PROFDIR)/cpu.out -run='^$$' -bench=. ./... &
	@sleep 2; echo "==> Collecting for 30s..."
	@sleep 30; kill %1 2>/dev/null || true
	go tool pprof -http=:8080 $(PROFDIR)/cpu.out 2>/dev/null &

profile-mem:
	@echo "==> Memory profile"
	@mkdir -p $(PROFDIR)
	$(GOTEST) -memprofile=$(PROFDIR)/mem.out -run='^$$' -bench=. ./...
	go tool pprof -http=:8080 $(PROFDIR)/mem.out 2>/dev/null &

profile-block:
	@echo "==> Block profile"
	@mkdir -p $(PROFDIR)
	$(GOTEST) -blockprofile=$(PROFDIR)/block.out -run='^$$' -bench=. ./...
	go tool pprof $(PROFDIR)/block.out

profile-trace:
	@echo "==> Execution trace: 5s"
	@mkdir -p $(PROFDIR)
	$(GOTEST) -trace=$(PROFDIR)/trace.out -run='^$$' -bench=. ./... &
	@sleep 5; kill %1 2>/dev/null || true
	go tool trace $(PROFDIR)/trace.out &

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
	@find . -path "*/testdata/fuzz/*" -type f 2>/dev/null | head -20

# ──────────────────────────────────────────────────────────────────────────────
# Coverage
# ──────────────────────────────────────────────────────────────────────────────

cover:
	@echo "==> Running tests with coverage"
	@mkdir -p $(COVERDIR)
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

cover-badge: cover
	@echo "==> Generating coverage badge"
	@COV=$$(go tool cover -func=$(COVERDIR)/cover.out | tail -1 | awk '{print $$NF}' | tr -d '%'); \
	echo "Coverage: $${COV}%"

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
# Security
# ──────────────────────────────────────────────────────────────────────────────

audit: vuln

vuln:
	@echo "==> Running govulncheck"
	@which govulncheck > /dev/null 2>&1 || (echo "Install: go install golang.org/x/vuln/cmd/govulncheck@latest" && exit 1)
	govulncheck ./...

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
# Code generation
# ──────────────────────────────────────────────────────────────────────────────

generate:
	@echo "==> Running go generate"
	go generate ./...

stringer:
	@echo "==> Running stringer"
	@which stringer > /dev/null 2>&1 || go install golang.org/x/tools/cmd/stringer@latest
	go generate ./...

# ──────────────────────────────────────────────────────────────────────────────
# Code metrics
# ──────────────────────────────────────────────────────────────────────────────

loc:
	@echo "==> Lines of code (Go files)"
	@find . -name '*.go' -not -path './vendor/*' -not -path './.git/*' | xargs wc -l | tail -1
	@echo ""
	@echo "── Per package ──────────────────────────────────────────"
	@for pkg in $$(find . -name '*.go' -not -path './vendor/*' -not -path './.git/*' -exec dirname {} \; | sort -u); do \
		count=$$(find $$pkg -maxdepth 1 -name '*.go' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $$1}'); \
		if [ "$$count" -gt 0 ] 2>/dev/null; then \
			echo "  $$count  $$pkg"; \
		fi; \
	done

complexity:
	@echo "==> Cyclomatic complexity"
	@which gocyclo > /dev/null 2>&1 || (echo "Install: go install github.com/fzipp/gocyclo/cmd/gocyclo@latest" && exit 1)
	gocyclo -top 20 .

# ──────────────────────────────────────────────────────────────────────────────
# Docker
# ──────────────────────────────────────────────────────────────────────────────

docker:
	@echo "==> Building Docker image"
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t lumen:$(VERSION) -t lumen:latest .

docker-run: docker
	@echo "==> Running Docker container"
	docker run --rm -it --network=host lumen:latest --chat

docker-shell: docker
	@echo "==> Opening Docker shell"
	docker run --rm -it --network=host --entrypoint /bin/sh lumen:latest

docker-push: docker
	@echo "==> Pushing Docker image"
	docker push lumen:$(VERSION)
	docker push lumen:latest

# ──────────────────────────────────────────────────────────────────────────────
# Release
# ──────────────────────────────────────────────────────────────────────────────

release: clean ci build-all
	@echo "==> Release binaries ready in dist/"
	@ls -lh dist/

release-snapshot: clean
	@echo "==> Building snapshot release"
	@which goreleaser > /dev/null 2>&1 || (echo "Install: go install github.com/goreleaser/goreleaser@latest" && exit 1)
	goreleaser release --snapshot --clean

tag:
	@echo "==> Creating git tag"
	@read -p "Tag version (vX.Y.Z): " tag; \
	git tag -a $$tag -m "Release $$tag" && \
	echo "==> Tagged $$tag — push with: git push origin $$tag"

# ──────────────────────────────────────────────────────────────────────────────
# Documentation
# ──────────────────────────────────────────────────────────────────────────────

docs:
	@echo "==> Generating documentation"
	@mkdir -p build/docs
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
	rm -rf bin/ build/ dist/

clean-all: clean
	@echo "==> Cleaning Go caches"
	go clean -cache -testcache
	rm -rf $(COVERDIR) $(BENCHDIR) $(PROFDIR)

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
	@echo ""
	@echo "── Binary ───────────────────────────────────────────────"
	@ls -lh $(BIN) 2>/dev/null || echo "Not built yet"

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
	@echo "    check          Static checks: fmt-check + vet + lint + audit"
	@echo ""
	@echo "  Setup:"
	@echo "    setup          Install hooks + development tools"
	@echo "    hooks-install  Install git pre-commit/push/commit-msg hooks"
	@echo "    hooks-uninstall Remove git hooks"
	@echo ""
	@echo "  Build:"
	@echo "    build          Build binary to bin/"
	@echo "    build-release  Build stripped/static release binary"
	@echo "    build-all      Cross-compile for all platforms (dist/)"
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
	@echo "    test-integration Run integration tests (requires Ollama)"
	@echo ""
	@echo "  Benchmarks:"
	@echo "    bench          Run benchmarks (save results)"
	@echo "    bench-mem      Run benchmarks with memory profile"
	@echo "    bench-cpu      Run benchmarks with CPU profile"
	@echo "    bench-compare  Compare benchmark results with benchstat"
	@echo ""
	@echo "  Profiling:"
	@echo "    profile        CPU profile (default)"
	@echo "    profile-cpu    CPU profile for 30s"
	@echo "    profile-mem    Memory profile"
	@echo "    profile-block  Block profile"
	@echo "    profile-trace  Execution trace for 5s"
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
	@echo "    cover-badge    Print coverage percentage"
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
	@echo "  Code Generation:"
	@echo "    generate       Run go generate"
	@echo "    stringer       Run stringer for enum types"
	@echo ""
	@echo "  Metrics:"
	@echo "    loc            Lines of code per package"
	@echo "    complexity     Top 20 cyclomatic complexity"
	@echo ""
	@echo "  Release:"
	@echo "    release        Clean + CI + cross-compile all platforms"
	@echo "    release-snapshot Build snapshot with goreleaser"
	@echo "    tag            Create a git tag interactively"
	@echo ""
	@echo "  Docker:"
	@echo "    docker         Build Docker image"
	@echo "    docker-run     Build and run in Docker (chat mode)"
	@echo "    docker-shell   Build and open shell in Docker"
	@echo "    docker-push    Push Docker image"
	@echo ""
	@echo "  Documentation:"
	@echo "    docs           Generate pkg docs to build/docs/"
	@echo "    godoc          Start local godoc server"
	@echo ""
	@echo "  Utilities:"
	@echo "    clean          Remove bin/, build/, dist/"
	@echo "    clean-all      Clean + Go caches"
	@echo "    env            Show build variables"
	@echo "    info           Show project and Go environment"
	@echo "    version        Run built binary --version"
	@echo "    help           Show this help"
	@echo ""
