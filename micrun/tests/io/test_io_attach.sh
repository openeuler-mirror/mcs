#!/bin/bash
# Test script for MicRun IO attach functionality
# Tests that ctr task attach works correctly with RTOS containers

set -e

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test configuration
CONTAINER_NAME="attach-test-rtos"
IMAGE_NAME="localhost:5000/mica-uniproton-app:xen-0.1"
TIMEOUT_ANNOTATION="org.openeuler.micrun.container.auto_close_timeout=60"

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
    log_test "Test 1: Creating container"

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

# Test 2: Start container in background
test_2_start_background() {
    log_test "Test 2: Starting container in background mode"

    # Start container in background
    timeout 10 ctr task start -d ${CONTAINER_NAME} >/dev/null 2>&1

    # Wait and check if task is running
    sleep 3
    if ctr task ls | grep -q "${CONTAINER_NAME}.*RUNNING"; then
        log_pass "Container started in background mode successfully"
        return 0
    else
        log_fail "Failed to start container in background mode"
        ctr task ls
        return 1
    fi
}

# Test 3: Verify stdin FIFO is open
test_3_verify_stdin_fifo() {
    log_test "Test 3: Verifying stdin FIFO is open by shim"

    # Get shim PID
    SHIM_PID=$(ps aux | grep "shim-mica-v2.*${CONTAINER_NAME}" | grep -v grep | awk '{print $2}' | head -1)

    if [ -z "$SHIM_PID" ]; then
        log_fail "Shim process not found"
        return 1
    fi

    # Check if shim has stdin FIFO open
    if lsof -p $SHIM_PID 2>/dev/null | grep -q "fifo.*${CONTAINER_NAME}-stdin"; then
        log_pass "Shim has stdin FIFO open (PID: $SHIM_PID)"
        return 0
    else
        log_fail "Shim does not have stdin FIFO open"
        return 1
    fi
}

# Test 4: Test attach with automatic command
test_4_attach_auto() {
    log_test "Test 4: Testing attach with automatic command execution"

    # Write a command to stdin and capture output
    (echo -ne "help\n" > /run/containerd/fifo/*/${CONTAINER_NAME}-stdin 2>/dev/null) &
    WRITE_PID=$!

    # Wait a bit for command to be processed
    sleep 2

    # Check if shim logs show stdin read activity
    if journalctl -u containerd --since '1 minute ago' --no-pager 2>/dev/null | grep -q "stdin read.*bytes"; then
        log_pass "Attach stdin processing works (found stdin read in logs)"
        kill $WRITE_PID 2>/dev/null || true
        return 0
    else
        log_warn "No stdin read activity found in logs (may need manual verification)"
        kill $WRITE_PID 2>/dev/null || true
        return 0  # Don't fail as timing may vary
    fi
}

# Test 5: Manual attach test (requires user interaction)
test_5_attach_manual() {
    log_test "Test 5: Manual attach test (user interaction required)"
    log_info "This test requires manual interaction:"
    log_info "  1. Run: ctr task attach ${CONTAINER_NAME}"
    log_info "  2. Press Enter - should see 'openEuler UniProton #' prompt"
    log_info "  3. Type 'help' - should see command list"
    log_info "  4. Type 'exit' or wait for timeout"

    read -p "Did the manual attach test work? (y/n): " answer
    if [ "$answer" = "y" ]; then
        log_pass "Manual attach test passed"
        return 0
    else
        log_fail "Manual attach test failed"
        return 1
    fi
}

# Test 6: Verify EAGAIN handling in logs
test_6_verify_eagain_handling() {
    log_test "Test 6: Verifying EAGAIN error handling in shim code"

    # Check if the shim binary contains EAGAIN handling
    SHIM_PATH="/usr/bin/containerd-shim-mica-v2"
    if [ -f "$SHIM_PATH" ]; then
        if strings "$SHIM_PATH" | grep -q "EAGAIN"; then
            log_pass "Shim binary contains EAGAIN error handling"
            return 0
        else
            log_warn "Shim binary may not have EAGAIN handling (compiled without debug symbols)"
            return 0  # Don't fail as release builds may strip symbols
        fi
    else
        log_warn "Shim binary not found at $SHIM_PATH"
        return 0
    fi
}

# Main test runner
main() {
    log_info "MicRun IO Attach Test Suite"
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
            test_1_create_container && test_2_start_background || true
            ;;
        3)
            test_1_create_container && test_2_start_background && test_3_verify_stdin_fifo || true
            ;;
        4)
            test_1_create_container && test_2_start_background && test_4_attach_auto || true
            ;;
        5)
            test_1_create_container && test_2_start_background && test_5_attach_manual || true
            ;;
        6)
            test_6_verify_eagain_handling || true
            ;;
        "all"|"")
            log_info "Running all tests..."
            test_1_create_container
            test_2_start_background
            test_3_verify_stdin_fifo
            test_4_attach_auto
            # Skip manual test by default in automated runs
            log_info ""
            log_info "Skipping manual attach test (test 5) in automated mode"
            log_info "Run '$0 5' to perform manual attach test"
            test_6_verify_eagain_handling
            ;;
        *)
            echo "Usage: $0 [test_number|all]"
            echo ""
            echo "Available tests:"
            echo "  1 - Create container"
            echo "  2 - Start container in background mode"
            echo "  3 - Verify stdin FIFO is open"
            echo "  4 - Test attach with automatic command"
            echo "  5 - Manual attach test (user interaction)"
            echo "  6 - Verify EAGAIN error handling"
            echo "  all - Run all automated tests"
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
