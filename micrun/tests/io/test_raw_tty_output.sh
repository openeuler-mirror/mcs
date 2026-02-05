#!/bin/bash
# Test to capture raw TTY output and analyze CRLF sequences
# Run this on remote host (192.168.7.2)

CONTAINER_NAME="analyze-$$"

echo "=========================================="
echo "Raw TTY Output Analysis"
echo "=========================================="

# Cleanup
ctr task ls -q | grep "$CONTAINER_NAME" && ctr task kill -s 9 "$CONTAINER_NAME" 2>/dev/null || true
ctr task ls -q | grep "$CONTAINER_NAME" && ctr task delete "$CONTAINER_NAME" 2>/dev/null || true
ctr container ls -q | grep "$CONTAINER_NAME" && ctr container delete "$CONTAINER_NAME" 2>/dev/null || true

# Create
echo "Creating container..."
ctr container create --runtime io.containerd.mica.v2 -t localhost:5000/mica-uniproton-app:xen-0.1 "$CONTAINER_NAME"

# Find TTY device
echo ""
echo "Starting container in background..."
# Start in background with a script that keeps stdin open
(sleep 45; ctr task kill -s 9 "$CONTAINER_NAME" 2>/dev/null) &
KEEPER_PID=$!

# Start task
ctr task start "$CONTAINER_NAME" </dev/null >/dev/null 2>&1 &
TASK_PID=$!

sleep 5  # Wait for TTY to be ready

# Find TTY device
TTY_DEVICE=$(ls -la /dev/ttyRPMSG* 2>/dev/null | grep "$CONTAINER_NAME" | awk '{print $NF}')

if [ -z "$TTY_DEVICE" ]; then
    echo "No TTY device found for $CONTAINER_NAME"
    echo "Available TTY devices:"
    ls -la /dev/ttyRPMSG* 2>/dev/null || echo "None"
    kill $KEEPER_PID 2>/dev/null
    exit 1
fi

echo "Found TTY: $TTY_DEVICE"
echo ""
echo "Reading TTY output (5 seconds)..."
echo "=========================================="

# Read TTY and analyze CRLF
timeout 5 cat "$TTY_DEVICE" 2>&1 | od -An -tx1 -v | head -50 || echo "End of output"

echo ""
echo "=========================================="
echo "Cleanup..."
kill $KEEPER_PID 2>/dev/null
wait $TASK_PID 2>/dev/null
ctr task kill -s 9 "$CONTAINER_NAME" 2>/dev/null || true
ctr task delete "$CONTAINER_NAME" 2>/dev/null || true
ctr container delete "$CONTAINER_NAME" 2>/dev/null || true

echo "Done"
