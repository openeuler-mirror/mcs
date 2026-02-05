#!/bin/bash
# MicRun IO Test Suite - Main Entry Point
# One command to run all IO tests and verify basic functionality
# Usage: ./run_all_io_tests.sh

# Don't use set -e as it interferes with command substitution in test functions

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE="${REMOTE_HOST:-root@192.168.7.2}"
IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"

# Color codes
PASS='\033[0;32m✓ PASS\033[0m'
FAIL='\033[0;31m✗ FAIL\033[0m'
SKIP='\033[0;33m○ SKIP\033[0m'
INFO='\033[0;34m'
NC='\033[0m'

# Results storage
declare -a TEST_NAMES=()
declare -a TEST_RESULTS=()
declare -a TEST_DETAILS=()
declare -a TEST_TIMES=()

# Helper functions
log_info() { echo -e "${INFO}[INFO]${NC} $1"; }
log_test() { echo -e "\n${INFO}▶ Testing:${NC} $1"; }

record_result() {
    local name="$1"
    local result="$2"
    local details="$3"
    local time="${4:-0}"

    TEST_NAMES+=("$name")
    TEST_RESULTS+=("$result")
    TEST_DETAILS+=("$details")
    TEST_TIMES+=("$time")
}

# Remote execution
remote() {
    ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 "$REMOTE" "$1"
}

# Cleanup
cleanup_all() {
    remote "
        pkill -9 ctr 2>/dev/null || true
        killall containerd-shim-mica-v2-arm64 2>/dev/null || true
        sleep 2
        rm -rf /run/containerd/io.containerd.runtime.v2.task/default/* 2>/dev/null || true
        for c in \$(ctr container ls -q 2>/dev/null); do
            ctr container delete \$c 2>/dev/null || true
        done
        # Also kill any remaining Xen domains
        for d in \$(xl list 2>/dev/null | awk '{print \$1}' | grep -v Name | grep -v Domain-0); do
            xl destroy \$d 2>/dev/null || true
        done
    " >/dev/null 2>&1
}

# ============================================
# Test 1: ctr background mode attach
# ============================================
test_1_ctr_background() {
    log_test "ctr background mode attach"
    local start
    local end
    local time
    local out
    local tmpfile=$(mktemp)
    start=$(date +%s)

    remote "
        ctr container create --runtime io.containerd.mica.v2 --annotation org.openeuler.micrun.container.auto_close_timeout=60s $IMAGE t1 2>&1 >/dev/null
        ctr task start -d t1 2>&1 >/dev/null
        sleep 3
        (echo 'help'; sleep 5) | ctr task attach t1 2>&1 || true
        sleep 1
        ctr task kill -s 9 t1 2>/dev/null || true
        sleep 1
        ctr container delete t1 2>/dev/null || true
    " > "$tmpfile" 2>&1
    out=$(cat "$tmpfile")
    rm -f "$tmpfile"

    end=$(date +%s)
    time=$((end - start))

    if echo "$out" | grep -q "support shell commond"; then
        record_result "ctr background mode attach" "PASS" "help output received" "$time"
        echo -e "$PASS"
    else
        record_result "ctr background mode attach" "FAIL" "No help output" "$time"
        echo -e "$FAIL"
    fi
}

# ============================================
# Test 2: ctr foreground mode (expect)
# ============================================
test_2_ctr_foreground() {
    log_test "ctr foreground mode (expect)"
    local start=$(date +%s)

    if expect "$SCRIPT_DIR/test_ctr_foreground.exp" >/dev/null 2>&1; then
        local end=$(date +%s)
        record_result "ctr foreground mode (expect)" "PASS" "Interactive session OK" "$((end - start))"
        echo -e "$PASS"
    else
        local end=$(date +%s)
        record_result "ctr foreground mode (expect)" "FAIL" "Expect script failed" "$((end - start))"
        echo -e "$FAIL"
    fi
}

# ============================================
# Test 3: nerdctl non-TTY mode
# ============================================
test_3_nerdctl_notty() {
    log_test "nerdctl non-TTY mode"
    local start
    local end
    local time
    local out
    local tmpfile=$(mktemp)
    start=$(date +%s)

    remote "
        ctr container create --runtime io.containerd.mica.v2 --annotation org.openeuler.micrun.container.auto_close_timeout=60s $IMAGE t3 2>&1 >/dev/null
        ctr task start -d t3 2>&1 >/dev/null
        sleep 3
        (echo 'help'; sleep 5) | ctr task attach t3 2>&1 || true
        sleep 1
        ctr task kill -s 9 t3 2>/dev/null || true
        sleep 1
        ctr container delete t3 2>/dev/null || true
    " > "$tmpfile" 2>&1
    out=$(cat "$tmpfile")
    # Debug: save output to a file for inspection
    echo "$out" > /tmp/test_3_debug.txt
    rm -f "$tmpfile"

    end=$(date +%s)
    time=$((end - start))

    if echo "$out" | grep -q "support shell commond"; then
        record_result "nerdctl non-TTY mode" "PASS" "help output received" "$time"
        echo -e "$PASS"
    else
        record_result "nerdctl non-TTY mode" "FAIL" "No help output" "$time"
        echo -e "$FAIL"
    fi
}

# ============================================
# Test 4: Multiple command execution
# ============================================
test_4_multiple_commands() {
    log_test "Multiple command execution"
    local start
    local end
    local time
    local out
    local tmpfile=$(mktemp)
    start=$(date +%s)

    remote "
        ctr container create --runtime io.containerd.mica.v2 $IMAGE t4 2>&1 >/dev/null
        ctr task start -d t4 2>&1 >/dev/null
        sleep 3
        (printf 'help\\nuname\\nexit\\n'; sleep 2) | timeout 15 ctr task attach t4 || true
        sleep 1
        ctr task kill -s 9 t4 2>/dev/null || true
        sleep 1
        ctr container delete t4 2>/dev/null || true
    " > "$tmpfile" 2>&1
    out=$(cat "$tmpfile")
    # Debug: save output to a file for inspection
    echo "$out" > /tmp/test_4_debug.txt
    rm -f "$tmpfile"

    end=$(date +%s)
    time=$((end - start))
    # Extract numeric value (handles newlines in output)
    local matches=$(echo "$out" | grep -cE "support shell commond|UniProton|openEuler" 2>/dev/null | tr -d '\n ' || echo 0)

    if [ "$matches" -ge 2 ]; then
        record_result "Multiple command execution" "PASS" "Multiple commands responded" "$time"
        echo -e "$PASS"
    else
        record_result "Multiple command execution" "FAIL" "Only $matches matches" "$time"
        echo -e "$FAIL"
    fi
}

# ============================================
# Test 5: Exit command detection
# ============================================
test_5_exit_command() {
    log_test "Exit command detection"
    local start
    local end
    local time
    local out
    local count
    start=$(date +%s)

    out=$(remote "
        ctr container create --runtime io.containerd.mica.v2 $IMAGE t5 2>&1 >/dev/null
        ctr task start -d t5 2>&1 >/dev/null
        sleep 3
        (printf 'help\\nexit\\n'; sleep 3) | timeout 15 ctr task attach t5 || true
        sleep 3
        ctr task ls | grep -c t5 || true
        ctr task kill -s 9 t5 2>/dev/null || true
        sleep 1
        ctr container delete t5 2>/dev/null || true
    " 2>/dev/null)

    end=$(date +%s)
    time=$((end - start))
    # Extract LAST numeric value - the count from grep -c is at the end
    count=$(echo "$out" | tr -d '\n' | grep -oE '[0-9]+' | tail -1 || echo 1)

    if [ "$count" -eq 0 ]; then
        record_result "Exit command detection" "PASS" "Container stopped on exit" "$time"
        echo -e "$PASS"
    else
        record_result "Exit command detection" "FAIL" "Container still running" "$time"
        echo -e "$FAIL"
    fi
}

# ============================================
# Test 6: Log cleanliness
# ============================================
test_6_log_cleanliness() {
    log_test "Log cleanliness (no spam)"
    local start
    local end
    local time
    local count
    start=$(date +%s)

    count=$(remote "
        ctr container create --runtime io.containerd.mica.v2 $IMAGE t6 2>&1 >/dev/null
        ctr task start -d t6 2>&1 >/dev/null
        sleep 3
        echo 'help' | timeout 10 ctr task attach t6 >/dev/null 2>&1 || true
        sleep 1
        tail -100 /var/log/mica/mica-runtime.log | grep -c 'stdin FIFO read' || echo 0
        ctr task kill -s 9 t6 2>/dev/null || true
        sleep 1
        ctr container delete t6 2>/dev/null || true
    " 2>/dev/null)

    end=$(date +%s)
    time=$((end - start))
    # Extract only numeric value (handles newlines in output)
    count=$(echo "$count" | tr -d '\n ' | grep -oE '[0-9]+' | head -1 || echo 0)

    if [ "$count" -lt 5 ]; then
        record_result "Log cleanliness" "PASS" "Only $count spam logs" "$time"
        echo -e "$PASS (${count} logs)"
    else
        record_result "Log cleanliness" "FAIL" "$count spam logs" "$time"
        echo -e "$FAIL (${count} logs)"
    fi
}

# ============================================
# Test 7: TTY echo suppression
# ============================================
test_7_tty_echo() {
    log_test "TTY echo suppression (no double echo)"
    local start=$(date +%s)

    if expect "$SCRIPT_DIR/test_tty_echo.exp" >/dev/null 2>&1; then
        local end=$(date +%s)
        record_result "TTY echo suppression" "PASS" "No double echo detected" "$((end - start))"
        echo -e "$PASS"
    else
        local end=$(date +%s)
        record_result "TTY echo suppression" "FAIL" "Double echo detected" "$((end - start))"
        echo -e "$FAIL"
    fi
}

# ============================================
# Print results table
# ============================================
print_results() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                    MicRun IO Test Results                            ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Remote: $REMOTE"
    echo "Image: $IMAGE"
    echo ""

    printf "+%-4s+%-60s+%-6s+%-8s+\n" "----" "------------------------------------------------------------" "------" "--------"
    printf "| %-2s | %-58s | %-4s | %-6s |\n" "No" "Test Name" "Time" "Result"
    printf "+%-4s+%-60s+%-6s+%-8s+\n" "----" "------------------------------------------------------------" "------" "--------"

    local passed=0
    local failed=0
    local skipped=0

    for i in "${!TEST_NAMES[@]}"; do
        local num=$((i + 1))
        local name="${TEST_NAMES[$i]}"
        local result="${TEST_RESULTS[$i]}"
        local time="${TEST_TIMES[$i]}s"

        # Color formatting
        local result_colored
        if [ "$result" = "PASS" ]; then
            result_colored="\e[32m✓ PASS\e[0m"
            ((passed++))
        elif [ "$result" = "SKIP" ]; then
            result_colored="\e[33m○ SKIP\e[0m"
            ((skipped++))
        else
            result_colored="\e[31m✗ FAIL\e[0m"
            ((failed++))
        fi

        printf "| %-2s | %-58s | %-4s | %-6s |\n" "$num" "$name" "$time" "$result_colored"
    done

    printf "+%-4s+%-60s+%-6s+%-8s+\n" "----" "------------------------------------------------------------" "------" "--------"
    echo ""
    echo "Summary: $passed passed, $failed failed, $skipped skipped, $((passed + failed + skipped)) total"
    echo ""

    # Show details for failed tests
    if [ $failed -gt 0 ]; then
        echo "╔══════════════════════════════════════════════════════════════════════╗"
        echo "║                      Failed Test Details                            ║"
        echo "╚══════════════════════════════════════════════════════════════════════╝"
        echo ""
        for i in "${!TEST_NAMES[@]}"; do
            if [ "${TEST_RESULTS[$i]}" = "FAIL" ]; then
                echo -e "${RED}✗ ${TEST_NAMES[$i]}${NC}"
                echo "  ${TEST_DETAILS[$i]}"
                echo ""
            fi
        done
    fi

    return $failed
}

# ============================================
# Main
# ============================================
main() {
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              MicRun IO Test Suite - One-Click Run                  ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""

    # Check connectivity
    if ! remote "echo 'connected'" >/dev/null 2>&1; then
        echo -e "${FAIL}Cannot connect to remote host: $REMOTE"
        exit 1
    fi
    log_info "Connected to remote host"
    echo ""

    # Cleanup before tests
    cleanup_all
    # Extra wait to ensure all resources are released
    sleep 10

    # Run all tests
    test_1_ctr_background
    # Quick cleanup after test 1
    remote "
        pkill -9 ctr 2>/dev/null || true
        killall containerd-shim-mica-v2-arm64 2>/dev/null || true
        sleep 1
        for c in \$(ctr container ls -q 2>/dev/null); do
            ctr container delete \$c 2>/dev/null || true
        done
    " 2>/dev/null
    sleep 2

    test_2_ctr_foreground
    sleep 5
    # Extra cleanup after expect test (test 2 uses -tt which may leave sessions)
    remote "
        pkill -9 ssh 2>/dev/null || true
        pkill -9 ctr 2>/dev/null || true
        killall containerd-shim-mica-v2-arm64 2>/dev/null || true
        sleep 2
        rm -rf /run/containerd/io.containerd.runtime.v2.task/default/* 2>/dev/null || true
        for c in \$(ctr container ls -q 2>/dev/null); do
            ctr container delete \$c 2>/dev/null || true
        done
        for d in \$(xl list 2>/dev/null | awk '\''{print \$1}'\'' | grep -v Name | grep -v Domain-0); do
            xl destroy \$d 2>/dev/null || true
        done
    " 2>/dev/null
    sleep 5

    test_3_nerdctl_notty
    # Extra cleanup after test 3
    remote "
        pkill -9 ctr 2>/dev/null || true
        killall containerd-shim-mica-v2-arm64 2>/dev/null || true
        sleep 2
        rm -rf /run/containerd/io.containerd.runtime.v2.task/default/* 2>/dev/null || true
        for c in \$(ctr container ls -q 2>/dev/null); do
            ctr container delete \$c 2>/dev/null || true
        done
        for d in \$(xl list 2>/dev/null | awk '\''{print \$1}'\'' | grep -v Name | grep -v Domain-0); do
            xl destroy \$d 2>/dev/null || true
        done
    " >/dev/null 2>&1
    sleep 5

    test_4_multiple_commands
    sleep 2
    test_5_exit_command
    sleep 2
    test_6_log_cleanliness
    sleep 2
    test_7_tty_echo

    # Print results
    print_results

    # Cleanup after tests
    cleanup_all

    # Exit with number of failures
    local fail_count=0
    for r in "${TEST_RESULTS[@]}"; do
        [ "$r" = "FAIL" ] && ((fail_count++))
    done

    if [ $fail_count -eq 0 ]; then
        echo -e "${PASS}All tests passed!"
        exit 0
    else
        echo -e "${FAIL}$fail_count test(s) failed!"
        exit 1
    fi
}

# Run main
main "$@"
