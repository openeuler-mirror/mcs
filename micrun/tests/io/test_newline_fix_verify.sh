#!/bin/bash
# Test script to verify newline fix for RTOS containers
# Run this on the remote host (192.168.7.2) where containerd is running
#
# Expected result: After "Hello, UniProton!" there should be 1-2 blank lines
# Before fix: 3+ blank lines (excessive)
# After fix: 1-2 blank lines (normal)

set -e

CONTAINER_NAME="test-nl-verify"

echo "=========================================="
echo "Newline Fix Verification Test"
echo "=========================================="

# Cleanup function
cleanup() {
    echo ""
    echo "--- Cleanup ---"
    ctr task kill -s 9 "$CONTAINER_NAME" 2>/dev/null || true
    ctr task delete "$CONTAINER_NAME" 2>/dev/null || true
    ctr container delete "$CONTAINER_NAME" 2>/dev/null || true
    ctr snapshot rm "$CONTAINER_NAME" 2>/dev/null || true
}

# Trap to ensure cleanup on exit
trap cleanup EXIT

# Initial cleanup
cleanup
sleep 2

# Create container
echo ""
echo "--- Create container ---"
ctr container create --runtime io.containerd.mica.v2 \
    -t localhost:5000/mica-uniproton-app:xen-0.1 "$CONTAINER_NAME"

# Start container and capture output
echo ""
echo "--- Start container ---"
echo "Waiting for RTOS to boot (60s timeout)..."
echo ""
echo "=========================================="

# Use script command for pseudo-terminal, timeout after 60s
timeout 60 script -q -c "ctr task start $CONTAINER_NAME" /dev/null 2>&1 || true

echo ""
echo "=========================================="
echo ""
echo "Verification complete!"
echo ""
echo "Check output above:"
echo "1. After 'Hello, UniProton!' - should be 1-2 blank lines (NOT 3+)"
echo "2. Prompt should appear without excessive spacing"
echo ""
echo "If you see 1-2 blank lines after Hello, the fix is working!"
echo "=========================================="
