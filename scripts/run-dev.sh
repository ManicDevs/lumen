#!/bin/bash
# Local development build loop for Lumen
# Uses spaces only - no tabs

PROJECT_ROOT="/home/cerberus/Desktop/lumen"
cd "$PROJECT_ROOT" || exit 1

echo "==> Available Memory: $(free -m | awk '/Mem:/ {print $7}') MB"
echo "==> Compiling native Go binaries..."

if go build -o bin/lumen ./cmd/lumen/main.go; then
    echo "==> Compilation complete. Launching binary..."
    echo "===================================================="
    ./bin/lumen --chat
else
    echo "==> Build error detected. Fix your Go types/syntax."
fi
