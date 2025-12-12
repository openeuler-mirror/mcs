#!/bin/bash
# Simple test script for mock_micad

set -e

echo "=== Mock Micad Test ==="

# Cleanup
pkill -9 mock_micad 2>/dev/null || true
sleep 1
rm -rf /tmp/mica

# Start mock_micad
echo "Starting mock_micad..."
./mock_micad > /tmp/mock.log 2>&1 &
MOCK_PID=$!
echo "Mock PID: $MOCK_PID"

sleep 2

# Test 1: Create client
echo ""
echo "Test 1: Creating client 'test1'"
echo -e "create test1" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
sleep 1

# Test 2: Check status
echo ""
echo "Test 2: Checking status"
echo -e "status" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
sleep 1

# Test 3: Start client
echo ""
echo "Test 3: Starting client 'test1'"
echo -e "start" | socat - UNIX-CONNECT:/tmp/mica/test1.socket
sleep 1

# Test 4: Stop client
echo ""
echo "Test 4: Stopping client 'test1'"
echo -e "stop" | socat - UNIX-CONNECT:/tmp/mica/test1.socket
sleep 1

# Cleanup
echo ""
echo "Stopping mock_micad..."
kill -TERM $MOCK_PID 2>/dev/null || true
sleep 1

# Show results
echo ""
echo "=== Results ==="
echo ""
echo "Mock micad output:"
cat /tmp/mock.log

# Check if resources were created
echo ""
echo "Checking resources:"
if [ -e /tmp/mica/test1.socket ]; then
    echo "✓ Client socket created"
else
    echo "✗ Client socket NOT found"
fi

if [ -e /tmp/mica/ttyRPMSG_test1 ]; then
    echo "✓ PTY symlink created"
else
    echo "✗ PTY symlink NOT found"
fi

if pgrep -f "test1" > /dev/null; then
    echo "✓ Shell process running"
else
    echo "? Shell process not found (might have been terminated)"
fi

echo ""
echo "=== Test Complete ==="
