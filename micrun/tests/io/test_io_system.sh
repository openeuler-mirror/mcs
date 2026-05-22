#!/bin/bash
# Test script for MicRun IO system validation
# Tests interactive terminal behavior, command output, signal handling, and attach/detach

set -e

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test configuration
CONTAINER_NAME="my-rtos"
IMAGE_NAME="docker.io/library/uniproton:latest"
TIMEOUT_ANNOTATION="org.openeuler.micrun.container.auto_close_timeout=120"

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

# Cleanup function
cleanup() {
    log_info "Cleaning up..."
    ctr task kill -s 9 ${CONTAINER_NAME} 2>/dev/null || true
    ctr task delete -f ${CONTAINER_NAME} 2>/dev/null || true
    ctr container delete ${CONTAINER_NAME} 2>/dev/null || true
    sleep 1
}

# Check if container exists
container_exists() {
    ctr container ls | grep -q "${CONTAINER_NAME}"
}

# Check if task is running
task_running() {
    ctr task ls | grep -q "${CONTAINER_NAME}"
}

# Test 1: Clean start - create and start container
test_1_create_container() {
    log_test "Test 1: Creating container with extended timeout"

    if container_exists; then
        log_warn "Container already exists, cleaning up..."
        cleanup
    fi

    ctr container create \
        --annotation ${TIMEOUT_ANNOTATION} \
        ${IMAGE_NAME} ${CONTAINER_NAME}

    if container_exists; then
        log_info "Container created successfully"
        return 0
    else
        log_error "Failed to create container"
        return 1
    fi
}

# Test 2: Interactive terminal - check Enter key response
test_2_interactive_terminal() {
    log_test "Test 2: Testing interactive terminal (Enter key response)"
    log_info "Expected: Pressing Enter should show 'UniProton #' prompt"
    log_info "This test requires manual interaction:"
    log_info "  1. After container starts, press Enter key"
    log_info "  2. Look for 'UniProton #' prompt"
    log_info "  3. Press Enter a few more times to verify repeated prompts"
    log_info "  4. Type 'help' command to check output"
    log_info "  5. Type 'exit' to exit"

    read -p "Press Enter to start the container in interactive mode..."

    # Start container with timeout (will auto-exit after annotation timeout)
    timeout 60 ctr task start ${CONTAINER_NAME} || true

    log_info "Interactive session ended"
    return 0
}

# Test 3: Background mode
test_3_background_mode() {
    log_test "Test 3: Testing background mode (-d flag)"

    # Make sure we have a fresh container
    if task_running; then
        log_warn "Task already running, killing it..."
        ctr task kill -s 9 ${CONTAINER_NAME} 2>/dev/null || true
        ctr task delete -f ${CONTAINER_NAME} 2>/dev/null || true
        sleep 1
    fi

    # Start in background
    ctr task start -d ${CONTAINER_NAME}

    if task_running; then
        log_info "Container started in background mode successfully"
        ctr task ls | grep ${CONTAINER_NAME}
        return 0
    else
        log_error "Failed to start container in background mode"
        return 1
    fi
}

# Test 4: Attach to running container
test_4_attach() {
    log_test "Test 4: Testing attach to running container"
    log_info "Expected: Should attach to running container terminal"

    read -p "Press Enter to attach to the container..."
    log_info "Attaching... (Type 'exit' for graceful stop, Ctrl+C to interrupt/stop, or Ctrl+P Ctrl+Q to detach)"

    timeout 30 ctr task attach ${CONTAINER_NAME} || true

    log_info "Attach session ended"
    return 0
}

# Test 5: Log inspection
test_5_inspect_logs() {
    log_test "Test 5: Inspecting MicRun debug logs"
    log_info "Checking for IO and TTY related log entries..."

    if [ -f "/var/log/mica/mica-runtime.log" ]; then
        log_info "Recent IO/TTY logs:"
        tail -100 /var/log/mica/mica-runtime.log | grep -E "\[IO\]|\[TTY\]" || log_warn "No IO/TTY logs found in recent output"
    else
        log_warn "Log file not found at /var/log/mica/mica-runtime.log"
    fi

    return 0
}

# Test 6: Verify NUL character filtering
test_6_check_nul_chars() {
    log_test "Test 6: Checking for NUL character issues"
    log_info "Run test 2 and verify no '^@' characters appear in output"
    log_info "If you see '^@' characters, the NUL filtering is not working"

    return 0
}

# Main test runner
main() {
    log_info "MicRun IO System Test Suite"
    log_info "================================"

    # Parse command line arguments
    TEST_NUM=""
    if [ $# -gt 0 ]; then
        TEST_NUM="$1"
    fi

    case "$TEST_NUM" in
        1)
            test_1_create_container
            ;;
        2)
            test_2_interactive_terminal
            ;;
        3)
            test_3_background_mode
            ;;
        4)
            test_4_attach
            ;;
        5)
            test_5_inspect_logs
            ;;
        6)
            test_6_check_nul_chars
            ;;
        "all"|"")
            cleanup
            test_1_create_container || { log_error "Test 1 failed"; exit 1; }
            test_2_interactive_terminal
            test_3_background_mode || { log_error "Test 3 failed"; exit 1; }
            test_4_attach
            test_5_inspect_logs
            test_6_check_nul_chars
            log_info "All automated tests completed. Manual verification required for tests 2, 4, 6."
            ;;
        *)
            echo "Usage: $0 [test_number|all]"
            echo ""
            echo "Available tests:"
            echo "  1 - Create container"
            echo "  2 - Interactive terminal (manual)"
            echo "  3 - Background mode"
            echo "  4 - Attach to container (manual)"
            echo "  5 - Inspect logs"
            echo "  6 - Check NUL character info"
            echo "  all - Run all tests"
            exit 1
            ;;
    esac
}

# Trap to ensure cleanup
trap cleanup EXIT

# Run main
main "$@"
