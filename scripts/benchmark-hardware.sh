#!/bin/bash
# Hardware validation benchmark for Lumen development environment
# Uses spaces only - no tabs

echo "==> Executing a 10-second multi-threaded CPU stress test..."
sysbench cpu --cpu-max-prime=20000 --threads=8 --time=10 run

echo ""
echo "==> Executing a 10 GB sequential memory write operation..."
sysbench memory --memory-block-size=1K --memory-total-size=10G --threads=8 run
