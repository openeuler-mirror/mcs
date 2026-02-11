#!/bin/bash
# Test script to verify extra newline fix
# Run this on the remote host (192.168.7.2) manually in an interactive shell
#
# This script creates a container and starts it interactively.
# You can then manually test by:
# 1. Pressing Enter - should show ONE prompt (no extra blank line)
# 2. Typing "help" - should show help output without excessive blank lines
# 3. Pressing Ctrl+P Ctrl+Q to detach

set -e

CONTAINER_NAME="test-newline-$$"

echo "=========================================="
echo "Extra Newline Fix - Manual Test"
echo "=========================================="
echo ""
echo "This script will:"
echo "1. Create a container"
echo "2. Start it interactively"
echo "3. You can manually test Enter key and help command"
echo ""
echo "Press Ctrl+P Ctrl+Q to detach when done testing"
echo ""

# Cleanup
echo "Cleanup..."
ctr task ls -q | grep -q "$CONTAINER_NAME" && ctr task kill -s 9 "$CONTAINER_NAME" 2>/dev/null || true
ctr task ls -q | grep -q "$CONTAINER_NAME" && ctr task delete "$CONTAINER_NAME" 2>/dev/null || true
ctr container ls -q | grep -q "$CONTAINER_NAME" && ctr container delete "$CONTAINER_NAME" || true

# Create
echo "Creating container..."
ctr container create --runtime io.containerd.mica.v2 \
    -t localhost:5000/mica-uniproton-app:xen-0.1 \
    "$CONTAINER_NAME"

# Start interactively
echo ""
echo "Starting container interactively..."
echo "=========================================="
echo "TEST INSTRUCTIONS:"
echo "1. Observe the 'Hello, UniProton!' message"
echo "2. Press Enter - should show ONE prompt (no extra blank line)"
echo "3. Type 'help' - output should NOT have excessive blank lines"
echo "4. Press Ctrl+P Ctrl+Q to detach"
echo "=========================================="
echo ""

ctr task start "$CONTAINER_NAME"

# Cleanup after detach
echo ""
echo "Cleaning up..."
ctr task kill -s 9 "$CONTAINER_NAME" 2>/dev/null || true
ctr task delete "$CONTAINER_NAME" 2>/dev/null || true
ctr container delete "$CONTAINER_NAME" 2>/dev/null || true

echo "Test complete!"
