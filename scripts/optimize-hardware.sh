#!/bin/bash
# Locks single-socket Xeon and memory profiles to max performance
# Uses spaces only - no tabs

echo "==> Masking default ondemand service..."
sudo systemctl mask ondemand

echo "==> Forcing all CPU threads into performance mode..."
sudo /bin/sh -c 'for g in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do echo performance > "$g"; done'

echo "==> Optimizing system memory limits for raw data throughput..."
sudo tee -a /etc/sysctl.conf > /dev/null << 'EOF_SYS'
vm.swappiness = 10
vm.dirty_background_ratio = 5
vm.dirty_ratio = 10
EOF_SYS

sudo sysctl -p
echo "==> Hardware performance profiles successfully locked."
