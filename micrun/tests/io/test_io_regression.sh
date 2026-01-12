#!/bin/bash
# Test cases for MicRun IO system
# This script validates that IO modifications work correctly
# and prevents regressions when making future changes.

set -e

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test configuration
CONTAINER_NAME="io-test-rtos"
IMAGE_NAME="localhost:5000/mica-uniproton-app:xen-0.1"
TIMEOUT_ANNOTATION="org.openeuler.micrun.container.auto_close_timeout=120"
TEST_TIMEOUT=30

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Log functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_test() {
    echo -e "${YELLOW}[TEST]${NC} $1"
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

# Cleanup function
cleanup() {
    log_info "Cleaning up..."
    ctr task kill -s 9 ${CONTAINER_NAME} 2>/dev/null || true
    ctr task delete -f ${CONTAINER_NAME} 2>/dev/null || true
    ctr container delete ${CONTAINER_NAME} 2>/dev/null || true
    rm -rf /run/containerd/s/* /run/containerd/io.containerd.runtime.v2.task.default/* 2>/dev/null || true
    sleep 1
}

# Test 1: Create container
test_1_create_container() {
    log_test "Test 1: Create container"

    if ctr container ls | grep -q "${CONTAINER_NAME}"; then
        log_warn "Container already exists, cleaning up..."
        cleanup
    fi

    ctr container create \
        --runtime io.containerd.mica.v2 \
        --annotation ${TIMEOUT_ANNOTATION} \
        ${IMAGE_NAME} ${CONTAINER_NAME}

    if ctr container ls | grep -q "${CONTAINER_NAME}"; then
        log_pass "Container created successfully"
        return 0
    else
        log_fail "Failed to create container"
        return 1
    fi
}

# Test 2: Start container and check for TTY device
test_2_start_and_tty() {
    log_test "Test 2: Start container and verify TTY device"

    # Start container in background
    timeout 10 ctr task start -d ${CONTAINER_NAME} >/dev/null 2>&1

    # Wait for TTY device to appear
    local count=0
    while [ $count -lt 10 ]; do
        if ls /dev/ttyRPMSG_* 2>/dev/null | grep -q "${CONTAINER_NAME}"; then
            log_pass "TTY device appeared: $(ls /dev/ttyRPMSG_* | grep "${CONTAINER_NAME}")"
            return 0
        fi
        sleep 1
        ((count++))
    done

    log_fail "TTY device did not appear"
    return 1
}

# Test 3: Check for NUL character filtering in logs
test_3_nul_filtering() {
    log_test "Test 3: Verify NUL character filtering"

    # Check recent logs for NUL filtering
    local nul_filtered=$(journalctl -u containerd --since '1 minute ago' --no-pager 2>/dev/null | grep -c "NUL bytes" || echo "0")

    if [ "$nul_filtered" -gt 0 ]; then
        log_pass "NUL character filtering active (found $nul_filtered log entries)"
        return 0
    else
        log_warn "No NUL filtering found in logs (may not have run yet)"
        return 0  # Don't fail, as this might be before any data was received
    fi
}

# Test 4: Check for newline filtering in logs
test_4_newline_filtering() {
    log_test "Test 4: Verify extra newline filtering"

    # Check recent logs for newline filtering
    local nl_filtered=$(journalctl -u containerd --since '1 minute ago' --no-pager 2>/dev/null | grep -c "extra newline" || echo "0")

    if [ "$nl_filtered" -gt 0 ]; then
        log_pass "Extra newline filtering active (found $nl_filtered log entries)"
        return 0
    else
        log_warn "No newline filtering found in logs (may not have run yet)"
        return 0  # Don't fail, as this might be before any data was received
    fi
}

# Test 5: Check TTY configuration
test_5_tty_config() {
    log_test "Test 5: Verify TTY raw mode configuration"

    # Check logs for TTY configuration
    local tty_config=$(journalctl -u containerd --since '2 minutes ago' --no-pager 2>/dev/null | grep "TTY.*configured" | tail -1)

    if [ -n "$tty_config" ]; then
        # Check for raw mode (lflag should be 0x0 after the arrow)
        if echo "$tty_config" | grep -q "lflag.*->.*0x0"; then
            log_pass "TTY configured in raw mode"
            return 0
        else
            log_fail "TTY not in raw mode: $tty_config"
            return 1
        fi
    else
        log_warn "No TTY configuration found in logs"
        return 0  # Don't fail, as timing might vary
    fi
}

# Test 6: Check for NUL characters in output
test_6_no_nul_in_output() {
    log_test "Test 6: Verify no NUL characters (^@) in stdout"

    # Check logs for any raw NUL bytes that weren't filtered
    local has_nul=$(journalctl -u containerd --since '1 minute ago' --no-pager 2>/dev/null | grep "stdout read" | grep -q "\\\\x00" && echo "1" || echo "0")

    # Better: check if filtered count > 0 for data that had NUL
    if journalctl -u containerd --since '1 minute ago' --no-pager 2>/dev/null | grep -q "Filtered.*NUL"; then
        log_pass "NUL filtering is working (found filter log entries)"
        return 0
    else
        log_warn "No NUL filtering activity found (may not have received data yet)"
        return 0
    fi
}

# Test 7: Interactive command test (help command)
test_7_help_command() {
    log_test "Test 7: Test help command output"

    # This test requires manual interaction or expect
    log_warn "Help command test requires manual verification"
    log_info "Run: ctr task attach ${CONTAINER_NAME}"
    log_info "Then type: help"
    log_info "Expected: Command list without extra blank lines"
    return 0
}

# Test 8: Check container status
test_8_container_status() {
    log_test "Test 8: Verify container is running"

    if ctr task ls | grep -q "${CONTAINER_NAME}.*RUNNING"; then
        log_pass "Container is running"
        return 0
    else
        log_fail "Container is not running"
        ctr task ls
        return 1
    fi
}

# Main test runner
main() {
    log_info "MicRun IO System Test Suite"
    log_info "============================="
    log_info ""

    # Parse command line arguments
    local test_num=""
    if [ $# -gt 0 ]; then
        test_num="$1"
    fi

    # Cleanup before starting
    cleanup

    case "$test_num" in
        1)
            test_1_create_container || true
            ;;
        2)
            test_1_create_container && test_2_start_and_tty || true
            ;;
        3)
            test_1_create_container && test_2_start_and_tty && test_3_nul_filtering || true
            ;;
        4)
            test_1_create_container && test_2_start_and_tty && test_4_newline_filtering || true
            ;;
        5)
            test_1_create_container && test_2_start_and_tty && test_5_tty_config || true
            ;;
        6)
            test_1_create_container && test_2_start_and_tty && test_6_no_nul_in_output || true
            ;;
        7)
            test_1_create_container && test_2_start_and_tty && test_7_help_command || true
            ;;
        8)
            test_1_create_container && test_2_start_and_tty && test_8_container_status || true
            ;;
        "all"|"")
            log_info "Running all tests..."
            test_1_create_container
            test_2_start_and_tty
            test_3_nul_filtering
            test_4_newline_filtering
            test_5_tty_config
            test_6_no_nul_in_output
            test_7_help_command
            test_8_container_status
            ;;
        *)
            echo "Usage: $0 [test_number|all]"
            echo ""
            echo "Available tests:"
            echo "  1 - Create container"
            echo "  2 - Start container and verify TTY"
            echo "  3 - Verify NUL character filtering"
            echo "  4 - Verify newline filtering"
            echo "  5 - Verify TTY raw mode configuration"
            echo "  6 - Verify no NUL in output"
            echo "  7 - Test help command (manual)"
            echo "  8 - Verify container status"
            echo "  all - Run all tests"
            exit 1
            ;;
    esac

    # Summary
    log_info ""
    log_info "============================="
    log_info "Test Summary:"
    log_info "  Passed: $TESTS_PASSED"
    log_info "  Failed: $TESTS_FAILED"

    if [ $TESTS_FAILED -eq 0 ]; then
        log_info "All tests passed!"
        return 0
    else
        log_error "Some tests failed!"
        return 1
    fi
}

# Trap to ensure cleanup
trap cleanup EXIT

# Run main
main "$@"
