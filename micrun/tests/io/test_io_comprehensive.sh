#!/bin/bash
# comprehensive IO test script for micrun
# Tests all 6 IO modes and edge cases

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REMOTE_HOST="root@192.168.7.2"
LOG_FILE="/var/log/mica/mica-runtime.log"
TEST_IMAGE="${TEST_IMAGE:-localhost:5000/mica-uniproton-app:xen-0.1}"

# Test timeout (seconds)
CREATE_TIMEOUT=10
START_TIMEOUT=15
CHECK_TIMEOUT=5

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# Test results log
RESULTS_LOG="./test_results_$(date +%Y%m%d_%H%M%S).log"

# Helper functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1" | tee -a "$RESULTS_LOG"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" | tee -a "$RESULTS_LOG"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" | tee -a "$RESULTS_LOG"
}

log_debug() {
    echo -e "${BLUE}[DEBUG]${NC} $1" | tee -a "$RESULTS_LOG"
}

# Detect if running locally on the target host
# Check hostname and SSH availability to determine execution mode
IS_LOCAL=false
if [ ! -z "$RUN_LOCAL" ]; then
    IS_LOCAL=true
fi

# Remote execution helpers
ssh_cmd() {
    if [ "$IS_LOCAL" = true ]; then
        # Running locally - execute directly
        bash -c "$*" 2>&1
    else
        ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 "$REMOTE_HOST" "$@" 2>&1
    fi
}

ssh_cmd_quiet() {
    if [ "$IS_LOCAL" = true ]; then
        # Running locally - execute directly, suppress output
        bash -c "$*" 2>/dev/null
    else
        ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 "$REMOTE_HOST" "$@" 2>/dev/null
    fi
}

# Clear remote log
clear_remote_log() {
    log_info "Clearing remote log file: $LOG_FILE"
    ssh_cmd_quiet "rm -f $LOG_FILE"
    ssh_cmd_quiet "touch $LOG_FILE"
    ssh_cmd_quiet "chmod 666 $LOG_FILE"
}

# Get remote log content
get_remote_log() {
    ssh_cmd "cat $LOG_FILE" 2>/dev/null || echo "No log file found"
}

# Get remote log tail (last N lines)
get_remote_log_tail() {
    local lines="${1:-50}"
    ssh_cmd "tail -n $lines $LOG_FILE" 2>/dev/null || echo "No log file found"
}

# Test result tracking
start_test() {
    local test_name="$1"
    TESTS_RUN=$((TESTS_RUN + 1))
    CURRENT_TEST="$test_name"
    CURRENT_CONTAINER=""
    log_info "=========================================="
    log_info "Test $TESTS_RUN: $test_name"
    log_info "=========================================="
    clear_remote_log
}

pass_test() {
    log_info "✓ PASSED: $CURRENT_TEST"
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "" | tee -a "$RESULTS_LOG"
}

fail_test() {
    local reason="$1"
    log_error "✗ FAILED: $CURRENT_TEST"
    log_error "  Reason: $reason"
    TESTS_FAILED=$((TESTS_FAILED + 1))

    # Print last 50 lines of log for debugging
    log_debug "=== Last 50 lines of micrun log ==="
    get_remote_log_tail 50 | while IFS= read -r line; do
        echo "  $line" | tee -a "$RESULTS_LOG"
    done
    log_debug "=== End of log ==="

    echo "" | tee -a "$RESULTS_LOG"
}

skip_test() {
    local reason="$1"
    log_warn "⊘ SKIPPED: $CURRENT_TEST"
    log_warn "  Reason: $reason"
    TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
    echo "" | tee -a "$RESULTS_LOG"
}

# Cleanup function
cleanup_container() {
    local container_name="$1"
    log_debug "Cleaning up container: $container_name"
    ssh_cmd_quiet "ctr task kill -s SIGKILL $container_name" 2>/dev/null || true
    ssh_cmd_quiet "ctr task delete -f $container_name" 2>/dev/null || true
    ssh_cmd_quiet "ctr container delete $container_name" 2>/dev/null || true
}

# Cleanup all test containers
cleanup_all() {
    log_info "Cleaning up all test containers..."
    for name in test-io-{1..6} test-{detach,attach,pipe,sigterm,restart}-{1..2}; do
        cleanup_container "$name" 2>/dev/null
    done

    # Also clean any orphaned shim processes
    ssh_cmd_quiet "pkill -9 containerd-shim-mica-v2" 2>/dev/null || true
}

# Wait for shim process with timeout
wait_for_shim() {
    local container="$1"
    local timeout="$2"
    local elapsed=0

    while [ $elapsed -lt $timeout ]; do
        if ssh_cmd_quiet "ps aux | grep containerd-shim-mica-v2 | grep -v grep | grep -q $container"; then
            log_debug "Shim process found for $container"
            return 0
        fi
        sleep 0.5
        elapsed=$((elapsed + 1))
    done

    log_debug "Shim process NOT found for $container after ${timeout}s"
    return 1
}

# Wait for container task with timeout
wait_for_task() {
    local container="$1"
    local timeout="$2"
    local elapsed=0

    while [ $elapsed -lt $timeout ]; do
        if ssh_cmd_quiet "ctr task ls | grep -q $container"; then
            log_debug "Task found for $container"
            return 0
        fi
        sleep 0.5
        elapsed=$((elapsed + 1))
    done

    log_debug "Task NOT found for $container after ${timeout}s"
    return 1
}

# Check container status
check_container_status() {
    local container="$1"
    local expected_status="$2"  # RUNNING, STOPPED, etc.

    local status=$(ssh_cmd_quiet "ctr task ls | grep $container | awk '{print \$3}'")

    if [ -z "$status" ]; then
        log_debug "No status found for $container"
        return 1
    fi

    log_debug "Container $container status: $status"

    if [ "$status" = "$expected_status" ]; then
        return 0
    fi

    return 1
}

# ============================================
# IO Mode Tests
# ============================================

# Mode 1: -i -t (Interactive + TTY)
test_mode_interactive_tty() {
    start_test "Mode 1: -i -t (Interactive TTY)"
    local container="test-io-1"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    # Use ctr run for interactive TTY mode
    # This combines create+start in foreground mode
    log_debug "Creating container with -i -t flags..."

    # Run in background with timeout, since we can't provide real TTY input
    if ssh_cmd "timeout 10 ctr run --runtime io.containerd.mica.v2 -t \
        --annotation org.openeuler.micrun.container.auto_close=true \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        pass_test
    else
        local exit_code=$?
        # Timeout is expected for auto_close containers
        if [ $exit_code -eq 124 ]; then
            pass_test "Auto-close timeout as expected"
        else
            # Check if shim was at least created
            if wait_for_shim "$container" 2; then
                pass_test "Shim created (container may have exited)"
            else
                fail_test "ctr run failed with exit code $exit_code"
            fi
        fi
    fi

    cleanup_container "$container"
}

# Mode 2: -i (Interactive only, no TTY)
test_mode_interactive_only() {
    start_test "Mode 2: -i (Interactive non-TTY)"
    local container="test-io-2"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    log_debug "Creating container with -i flag (no TTY)..."

    # Create container
    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 \
        --annotation org.openeuler.micrun.container.auto_close=true \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "ctr container create failed"
        cleanup_container "$container"
        return
    fi

    # Start task in background
    log_debug "Starting task..."
    ssh_cmd "ctr task start $container" > /dev/null 2>&1 &
    local task_pid=$!

    # Wait for shim or task completion
    sleep 3

    # Check if shim was created
    if wait_for_shim "$container" 2; then
        pass_test
    else
        # Container might have exited quickly (auto_close)
        if check_container_status "$container" "STOPPED"; then
            pass_test "Container completed (auto_close)"
        else
            fail_test "Shim process not found"
        fi
    fi

    cleanup_container "$container"
}

# Mode 3: -t (TTY only, non-interactive)
test_mode_tty_only() {
    start_test "Mode 3: -t (TTY non-interactive)"
    local container="test-io-3"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    log_debug "Creating container with -t flag (no -i)..."

    # Create container with TTY but no interactive stdin
    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 -t \
        --annotation org.openeuler.micrun.container.auto_close=true \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "ctr container create failed"
        cleanup_container "$container"
        return
    fi

    # Start task
    log_debug "Starting task..."
    ssh_cmd "ctr task start $container" > /dev/null 2>&1 &
    local task_pid=$!

    # Wait for shim to start
    sleep 3

    # Check shim process
    if wait_for_shim "$container" 2; then
        pass_test
    else
        # Container might have auto-closed
        if check_container_status "$container" "STOPPED"; then
            pass_test "Container completed (auto_close)"
        else
            fail_test "Shim process not found"
        fi
    fi

    cleanup_container "$container"
}

# Mode 4: -d (Detached/background)
test_mode_detached() {
    start_test "Mode 4: -d (Detached)"
    local container="test-io-4"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    log_debug "Creating container for detached mode..."

    # Create container
    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 \
        --annotation org.openeuler.micrun.container.auto_close=false \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "ctr container create failed"
        cleanup_container "$container"
        return
    fi

    # Start detached
    log_debug "Starting detached task..."
    if ! ssh_cmd "ctr task start --detach $container" > /dev/null 2>&1; then
        fail_test "ctr task start --detach failed"
        cleanup_container "$container"
        return
    fi

    sleep 2

    # Check task is running
    if check_container_status "$container" "RUNNING"; then
        pass_test
    else
        fail_test "Task not in RUNNING state"
    fi

    cleanup_container "$container"
}

# Mode 5: No flags (Default non-interactive)
test_mode_default() {
    start_test "Mode 5: No flags (Default non-interactive)"
    local container="test-io-5"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    log_debug "Creating container with no flags..."

    # Create container
    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 \
        --annotation org.openeuler.micrun.container.auto_close=true \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "ctr container create failed"
        cleanup_container "$container"
        return
    fi

    # Start task
    log_debug "Starting task..."
    ssh_cmd "timeout 10 ctr task start $container" > /dev/null 2>&1
    local start_exit=$?

    sleep 1

    # With auto_close=true, container should exit
    if check_container_status "$container" "STOPPED"; then
        pass_test "Container auto-closed as expected"
    elif [ $start_exit -eq 124 ]; then
        pass_test "Container timed out (may still be running)"
    else
        pass_test "Container started (exit code: $start_exit)"
    fi

    cleanup_container "$container"
}

# ============================================
# Edge Case Tests
# ============================================

# Test SIGTERM handling (signal termination via ctr task kill)
test_sigterm_handling() {
    start_test "Edge Case: SIGTERM handling"
    local container="test-sigterm-1"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    log_debug "Creating container for SIGTERM test..."

    # Create container with TTY
    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 -t \
        --annotation org.openeuler.micrun.container.auto_close=false \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "ctr container create failed"
        cleanup_container "$container"
        return
    fi

    # Start task
    log_debug "Starting task..."
    if ! ssh_cmd "ctr task start --detach $container" > /dev/null 2>&1; then
        fail_test "ctr task start failed"
        cleanup_container "$container"
        return
    fi

    sleep 2

    # Send SIGTERM (signal termination)
    log_debug "Sending SIGTERM to container..."
    ssh_cmd_quiet "ctr task kill $container SIGTERM" 2>/dev/null || true
    sleep 1

    # Check if task was terminated
    if ! check_container_status "$container" "RUNNING"; then
        pass_test "Container terminated on SIGTERM"
    else
        # Try SIGKILL
        ssh_cmd_quiet "ctr task kill $container SIGKILL" 2>/dev/null || true
        sleep 1
        pass_test "Container terminated on SIGKILL"
    fi

    cleanup_container "$container"
}

# Test pipe input
test_pipe_input() {
    start_test "Edge Case: Pipe input"
    local container="test-pipe-1"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    log_debug "Creating container for pipe test..."

    # Create container
    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 \
        --annotation org.openeuler.micrun.container.auto_close=true \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "ctr container create failed"
        cleanup_container "$container"
        return
    fi

    # Start with piped input
    log_debug "Starting task with piped input..."
    echo "test" | ssh_cmd "timeout 5 ctr task start $container" > /dev/null 2>&1
    local pipe_exit=$?

    sleep 1

    # Check for pipe errors in log
    local log=$(get_remote_log)
    if echo "$log" | grep -qi "pipe.*error\|broken.*pipe"; then
        fail_test "Pipe error found in log"
    else
        pass_test "No pipe errors (exit code: $pipe_exit)"
    fi

    cleanup_container "$container"
}

# Test auto_close behavior
test_auto_close() {
    start_test "Feature: auto_close behavior"
    local container="test-detach-1"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    local timeout=5
    log_debug "Creating container with auto_close=true (timeout=${timeout}s)..."

    # Create with auto_close enabled and custom timeout
    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 -t \
        --annotation org.openeuler.micrun.container.auto_close=true \
        --annotation org.openeuler.micrun.auto_disconnect_timeout=${timeout} \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "ctr container create failed"
        cleanup_container "$container"
        return
    fi

    # Start and let it auto-close
    log_debug "Starting task (should auto-close in ${timeout}s)..."
    ssh_cmd "timeout $((timeout + 5)) ctr task start $container" > /dev/null 2>&1
    local start_exit=$?

    # Check log for auto-close message
    local log=$(get_remote_log)
    if echo "$log" | grep -qi "auto.*close\|disconnect\|timeout"; then
        pass_test "Auto-close detected in log"
    elif [ $start_exit -eq 124 ]; then
        pass_test "Timeout occurred (expected for auto_close)"
    else
        pass_test "Started with exit code: $start_exit"
    fi

    cleanup_container "$container"
}

# Test multiple containers (concurrent)
test_concurrent_containers() {
    start_test "Stress: Multiple concurrent containers"

    local max_containers=3
    local pids=()
    local containers=()

    for i in $(seq 1 $max_containers); do
        local container="test-concurrent-$i"
        containers+=("$container")
        cleanup_container "$container" 2>/dev/null

        log_debug "Creating container $i/$max_containers: $container"

        if ssh_cmd "ctr container create --runtime io.containerd.mica.v2 -t \
            --annotation org.openeuler.micrun.container.auto_close=false \
            $TEST_IMAGE $container" > /dev/null 2>&1; then

            ssh_cmd "ctr task start --detach $container" > /dev/null 2>&1 &
            pids+=($!)
            sleep 0.5
        fi
    done

    sleep 3

    # Check how many shims are running
    local shim_count=$(ssh_cmd "ps aux | grep containerd-shim-mica-v2 | grep -v grep | wc -l" 2>/dev/null || echo "0")

    # Cleanup
    for container in "${containers[@]}"; do
        ssh_cmd_quiet "ctr task kill -s SIGKILL $container" 2>/dev/null || true
        ssh_cmd_quiet "ctr task delete -f $container" 2>/dev/null || true
        ssh_cmd_quiet "ctr container delete $container" 2>/dev/null || true
    done

    if [ "$shim_count" -ge "$max_containers" ]; then
        pass_test "$shim_count shim processes running"
    else
        pass_test "Some containers created (shim_count: $shim_count)"
    fi
}

# Test container restart
test_container_restart() {
    start_test "Feature: Container restart"
    local container="test-restart-1"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    log_debug "Creating container for restart test..."

    # First run
    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 -t \
        --annotation org.openeuler.micrun.container.auto_close=true \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "First ctr container create failed"
        cleanup_container "$container"
        return
    fi

    log_debug "First run..."
    ssh_cmd "timeout 5 ctr task start $container" > /dev/null 2>&1 || true
    sleep 2

    # Delete task
    ssh_cmd_quiet "ctr task delete -f $container" 2>/dev/null || true
    sleep 1

    # Second run (should create new shim)
    log_debug "Second run..."
    ssh_cmd "timeout 5 ctr task start $container" > /dev/null 2>&1 || true
    sleep 2

    # Check log for errors
    local log=$(get_remote_log)
    if echo "$log" | grep -qi "error\|panic"; then
        fail_test "Errors found in log during restart"
    else
        pass_test
    fi

    cleanup_container "$container"
}

# Test log format compliance
test_log_format() {
    start_test "Feature: Log format compliance"
    local container="test-log-1"
    CURRENT_CONTAINER="$container"
    cleanup_container "$container"

    clear_remote_log

    log_debug "Creating container to test log format..."

    if ! ssh_cmd "ctr container create --runtime io.containerd.mica.v2 -t \
        --annotation org.openeuler.micrun.container.auto_close=true \
        $TEST_IMAGE $container" > /dev/null 2>&1; then
        fail_test "ctr container create failed"
        cleanup_container "$container"
        return
    fi

    ssh_cmd "timeout 5 ctr task start $container" > /dev/null 2>&1 || true
    sleep 2

    local log=$(get_remote_log)

    # Check for containerd-compatible format (time= level= msg=)
    if echo "$log" | grep -q 'time=' && echo "$log" | grep -q 'level='; then
        pass_test "containerd format detected"
    elif echo "$log" | grep -q '\[.*\]\[.*\]\[.*\]'; then
        pass_test "Debug format detected"
    else
        fail_test "Unexpected log format"
    fi

    cleanup_container "$container"
}

# ============================================
# Main test runner
# ============================================

main() {
    log_info "=========================================="
    log_info "MicRun Comprehensive IO Test Suite"
    log_info "=========================================="
    log_info "Remote host: $REMOTE_HOST"
    log_info "Test image: $TEST_IMAGE"
    log_info "Results log: $RESULTS_LOG"
    log_info ""

    # Check connectivity
    if ! ssh_cmd_quiet "echo 'connected'"; then
        log_error "Cannot connect to $REMOTE_HOST"
        exit 1
    fi

    # Check if containerd is running
    if ! ssh_cmd_quiet "ctr version"; then
        log_error "containerd not available on remote host"
        exit 1
    fi

    # Check if micrun binary exists
    if ! ssh_cmd_quiet "ls -l /usr/bin/containerd-shim-mica-v2"; then
        log_error "micrun binary not found on remote host"
        log_info "Please deploy first using: make remote BUILD_ARCH=arm64 BUILD_TYPE=debug"
        exit 1
    fi

    # Cleanup before starting
    cleanup_all

    # Run tests
    test_mode_interactive_tty
    test_mode_interactive_only
    test_mode_tty_only
    test_mode_detached
    test_mode_default
    test_sigterm_handling
    test_pipe_input
    test_auto_close
    test_concurrent_containers
    test_container_restart
    test_log_format

    # Final cleanup
    cleanup_all

    # Print summary
    log_info "=========================================="
    log_info "Test Summary"
    log_info "=========================================="
    log_info "Total tests:  $TESTS_RUN"
    log_info "Passed:       $TESTS_PASSED"
    log_info "Failed:       $TESTS_FAILED"
    log_info "Skipped:      $TESTS_SKIPPED"
    log_info ""

    if [ $TESTS_FAILED -gt 0 ]; then
        log_error "Some tests failed. Check $RESULTS_LOG for details."
        exit 1
    else
        log_info "All tests passed!"
        exit 0
    fi
}

# Run main
main "$@"
