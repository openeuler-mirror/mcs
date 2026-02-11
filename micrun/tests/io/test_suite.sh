#!/bin/bash
# MicRun IO Test Suite - Simple and Reliable
# Tests are run individually to avoid interference

set -e

REMOTE="${REMOTE_HOST:-root@192.168.7.2}"
IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"

# Colors
PASS='\033[0;32m✓ PASS\033[0m'
FAIL='\033[0;31m✗ FAIL\033[0m'
NC='\033[0m'

# Cleanup
cleanup() {
    ssh "$REMOTE" "
        pkill -9 ctr 2>/dev/null || true
        sleep 1
        ctr container delete \$(ctr container ls -q) 2>/dev/null || true
    " 2>/dev/null || true
}

# Test functions
test_1_ctr_background() {
    echo -n "Test 1: ctr background mode attach... "
    out=$(ssh "$REMOTE" "
        ctr container create --runtime io.containerd.mica.v2 $IMAGE t1 2>&1 >/dev/null
        ctr task start -d t1 2>&1 >/dev/null
        sleep 2
        echo help | timeout 5 ctr task attach t1 2>&1
        ctr task kill -s 9 t1 2>/dev/null || true
        ctr container delete t1 2>/dev/null || true
    " 2>&1)

    if echo "$out" | grep -q "support shell commond"; then
        echo -e "$PASS"
        return 0
    else
        echo -e "$FAIL"
        echo "  Output: $(echo "$out" | head -3)"
        return 1
    fi
}

test_2_nerdctl_tty() {
    echo -n "Test 2: nerdctl TTY mode (-it)... "
    out=$(ssh "$REMOTE" "
        timeout 10 sh -c '(echo help; sleep 2) | nerdctl run -it --rm --runtime io.containerd.mica.v2 $IMAGE' 2>&1 || true
    " 2>&1)

    if echo "$out" | grep -q "support shell commond"; then
        echo -e "$PASS"
        return 0
    else
        echo -e "$FAIL"
        echo "  Output: $(echo "$out" | head -3)"
        return 1
    fi
}

test_3_nerdctl_notty() {
    echo -n "Test 3: nerdctl non-TTY mode (-i)... "
    out=$(ssh "$REMOTE" "
        timeout 10 sh -c '(echo help; sleep 2) | nerdctl run -i --rm --runtime io.containerd.mica.v2 $IMAGE' 2>&1 || true
    " 2>&1)

    if echo "$out" | grep -q "support shell commond"; then
        echo -e "$PASS"
        return 0
    else
        echo -e "$FAIL"
        echo "  Output: $(echo "$out" | head -3)"
        return 1
    fi
}

test_4_multiple_commands() {
    echo -n "Test 4: Multiple command execution... "
    out=$(ssh "$REMOTE" "
        ctr container create --runtime io.containerd.mica.v2 $IMAGE t4 2>&1 >/dev/null
        ctr task start -d t4 2>&1 >/dev/null
        sleep 2
        (printf 'help\nuname\nexit\n'; sleep 2) | timeout 10 ctr task attach t4 2>&1 || true
        ctr task kill -s 9 t4 2>/dev/null || true
        ctr container delete t4 2>/dev/null || true
    " 2>&1)

    local matches=$(echo "$out" | grep -cE "support shell commond|UniProton|openEuler" || echo 0)
    if [ "$matches" -ge 2 ]; then
        echo -e "$PASS"
        return 0
    else
        echo -e "$FAIL"
        echo "  Matches: $matches/2, Output: $(echo "$out" | head -3)"
        return 1
    fi
}

test_5_log_cleanliness() {
    echo -n "Test 5: Log cleanliness... "
    count=$(ssh "$REMOTE" "
        ctr container create --runtime io.containerd.mica.v2 $IMAGE t5 2>&1 >/dev/null
        ctr task start -d t5 2>&1 >/dev/null
        sleep 2
        echo help | timeout 5 ctr task attach t5 >/dev/null 2>&1 || true
        sleep 1
        tail -100 /var/log/mica/mica-runtime.log | grep -c 'stdin FIFO read' || echo 0
        ctr task kill -s 9 t5 2>/dev/null || true
        ctr container delete t5 2>/dev/null || true
    " 2>/dev/null)

    if [ "$count" -lt 5 ]; then
        echo -e "$PASS (${count} logs)"
        return 0
    else
        echo -e "$FAIL (${count} logs)"
        return 1
    fi
}

test_6_exit_command() {
    echo -n "Test 6: Exit command detection... "
    out=$(ssh "$REMOTE" "
        ctr container create --runtime io.containerd.mica.v2 $IMAGE t6 2>&1 >/dev/null
        ctr task start -d t6 2>&1 >/dev/null
        sleep 2
        (printf 'help\nexit\n'; sleep 2) | timeout 10 ctr task attach t6 2>&1 || true
        sleep 1
        ctr task ls | grep -c t6 || echo 0
        ctr task kill -s 9 t6 2>/dev/null || true
        ctr container delete t6 2>/dev/null || true
    " 2>/dev/null)

    if [ "$out" -eq 0 ]; then
        echo -e "$PASS (container stopped)"
        return 0
    else
        echo -e "$FAIL (container still running)"
        return 1
    fi
}

# Main
echo "╔════════════════════════════════════════════════════════════╗"
echo "║              MicRun IO Test Suite                          ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""
echo "Remote: $REMOTE"
echo "Image: $IMAGE"
echo ""

cleanup
sleep 1

# Run tests
pass=0
fail=0

test_1_ctr_background && ((pass++)) || ((fail++))
test_2_nerdctl_tty && ((pass++)) || ((fail++))
test_3_nerdctl_notty && ((pass++)) || ((fail++))
test_4_multiple_commands && ((pass++)) || ((fail++))
test_5_log_cleanliness && ((pass++)) || ((fail++))
test_6_exit_command && ((pass++)) || ((fail++))

# Summary
echo ""
echo "╔════════════════════════════════════════════════════════════╗"
echo "║                      Results                              ║"
echo "╠════════════════════════════════════════════════════════════╣"
echo "║  Total: 6                                                 ║"
printf "║  \033[32mPassed: %d\033[0m                                               ║\n" "$pass"
printf "║  \033[31mFailed: %d\033[0m                                               ║\n" "$fail"
echo "╚════════════════════════════════════════════════════════════╝"

cleanup
exit $fail
