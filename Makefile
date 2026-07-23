# ==============================================================================
# Lumen - High Performance Build System Configuration (Spaces Only)
# ==============================================================================

BINARY_NAME = lumen
ENTRY_POINT = ./cmd/lumen/main.go

# Standard configuration defaults
GO = go
GOFLAGS = -trimpath

# --- High-Performance Tooling Configurations ---
# -s -w: Strips all DWARF debugging structures and symbols to minimize binary size.
LDFLAGS = -ldflags="-s -w"

# -B: Disables Go runtime array/slice bounds checking. 
# WARNING: Only use this flag if the test suite passes cleanly.
PERF_GCFLAGS = -gcflags="all=-B"

.PHONY: all build fast-build run-fast test clean help

all: test build

build:
	@echo "==> Building optimized deployment binary..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o ./bin/$(BINARY_NAME) $(ENTRY_POINT)
	@echo "==> Build complete: ./bin/$(BINARY_NAME)"

fast-build:
	@echo "==> Building ultra-performance binary (Bypassing Bounds Checking)..."
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) $(LDFLAGS) $(PERF_GCFLAGS) -o $(BINARY_NAME) $(ENTRY_POINT)
	@echo "==> Ultra-performance build complete: ./$(BINARY_NAME)"

run-fast: fast-build
	@echo "==> Launching Lumen in high-performance harvesting mode..."
	@if [ -z "$(TARGET)" ]; then \
	    echo "ERROR: Please specify a harvest target directory. Example: make run-fast TARGET=./internal"; \
	    exit 1; \
	fi
	./$(BINARY_NAME) $(TARGET)

test:
	@echo "==> Running internal verification test suites..."
	$(GO) test -v -race ./...

clean:
	@echo "==> Cleaning local workspace targets..."
	rm -f $(BINARY_NAME)
	$(GO) clean -cache

help:
	@echo "Available commands:"
	@echo "  build       - Build the production binary using default optimizations"
	@echo "  fast-build  - Build with unsafe hardware optimization overrides (No Bounds Checking)"
	@echo "  run-fast    - Launch lumen against a path using maximum execution throughput"
	@echo "  test        - Execute complete package verification suites"
	@echo "  clean       - Wipe out built execution files and temporary build structures"
