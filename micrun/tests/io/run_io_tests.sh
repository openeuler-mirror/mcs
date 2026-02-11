#!/bin/bash
# Comprehensive IO Test Runner for MicRun
# Runs all IO tests and presents results in table format

set -e

# Test configuration
REMOTE_HOST="${REMOTE_HOST:-root@192.168.7.2}"
IMAGE_NAME="${IMAGE_NAME:-localhost:5000/mica-uniproton-app:xen-0.1}"
TEST_TIMEOUT="${TEST_TIMEOUT:-30}"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Test results storage
declare -a TEST_NAMES=()
declare -a TEST_RESULTS=()  # "PASS" or "FAIL"
declare -a TEST_TIMES=()
declare -a TEST_DETAILS=()

# Remote execution function
run_remote() {
    ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 "${REMOTE_HOST}" "$1"
}

# Cleanup function
cleanup() {
    echo -e "${BLUE}[CLEAN]${NC} Cleaning up containers..."
    run_remote "ctr task kill -s 9 io-test 2>/dev/null || true; \
                ctr task delete -f io-test 2>/dev/null || true; \
                ctr container delete io-test 2>/dev/null || true; \
                rm -rf /run/containerd/io.containerd.runtime.v2.task/default/io-test 2>/dev/null || true" 2>/dev/null || true
    sleep 1
}

# Log functions
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_test() { echo -e "${CYAN}[TEST]${NC} $1"; }
log_pass() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; }

# Record test result
record_result() {
    local name="$1"
    local result="$2"
    local time="$3"
    local details="$4"

    TEST_NAMES+=("$name")
    TEST_RESULTS+=("$result")
    TEST_TIMES+=("$time")
    TEST_DETAILS+=("$details")
}

# Print test results table
print_results_table() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                    MicRun IO Test Results                            ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""
    printf "+%-4s+%-70s+%-6s+%-8s+\n" "----" "----------------------------------------------------------------------" "------" "--------"
    printf "| %-2s | %-68s | %-4s | %-6s |\n" "No" "Test Name" "Time" "Result"
    printf "+%-4s+%-70s+%-6s+%-8s+\n" "----" "----------------------------------------------------------------------" "------" "--------"

    local passed=0
    local failed=0

    for i in "${!TEST_NAMES[@]}"; do
        local name="${TEST_NAMES[$i]}"
        local result="${TEST_RESULTS[$i]}"
        local time="${TEST_TIMES[$i]}"

        # Truncate name if too long
        local display_name="${name:0:68}"

        if [ "$result" = "PASS" ]; then
            printf "| %-2s | \e[32m%-68s\e[0m | %-4s | \e[32m%-6s\e[0m |\n" "$((i+1))" "$display_name" "${time}s" "✓ PASS"
            ((passed++))
        else
            printf "| %-2s | \e[31m%-68s\e[0m | %-4s | \e[31m%-6s\e[0m |\n" "$((i+1))" "$display_name" "${time}s" "✗ FAIL"
            ((failed++))
        fi
    done

    printf "+%-4s+%-70s+%-6s+%-8s+\n" "----" "----------------------------------------------------------------------" "------" "--------"
    echo ""
    echo "Summary: $passed passed, $failed failed, $((passed+failed)) total"
    echo ""

    # Print details for failed tests
    if [ $failed -gt 0 ]; then
        echo "╔══════════════════════════════════════════════════════════════════════╗"
        echo "║                       Failed Test Details                            ║"
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
# Test 1: ctr background mode attach
# ============================================
test_ctr_background_attach() {
    local start_time=$(date +%s)

    log_test "Test 1: ctr background mode attach (ctr task start -d + ctr task attach)"

    local output=$(run_remote "
        cleanup() {
            ctr task kill -s 9 io-test 2>/dev/null || true
            ctr task delete -f io-test 2>/dev/null || true
            ctr container delete io-test 2>/dev/null || true
            rm -rf /run/containerd/io.containerd.runtime.v2.task/default/io-test 2>/dev/null || true
        }
        cleanup
        ctr container create --runtime io.containerd.mica.v2 ${IMAGE_NAME} io-test
        ctr task start -d io-test
        sleep 2
        echo 'help' | timeout 5 ctr task attach io-test 2>&1 | head -20
        cleanup
    " 2>&1)

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    if echo "$output" | grep -q "support shell commond"; then
        record_result "ctr background mode attach" "PASS" "${duration}" "help command output received"
        log_pass "ctr background attach works"
        return 0
    else
        record_result "ctr background mode attach" "FAIL" "${duration}" "No expected output. Output: ${output}"
        log_fail "ctr background attach failed"
        return 1
    fi
}

# ============================================
# Test 2: ctr foreground mode
# ============================================
test_ctr_foreground() {
    local start_time=$(date +%s)

    log_test "Test 2: ctr foreground mode (ctr task start)"

    local output=$(run_remote "
        cleanup() {
            ctr task kill -s 9 io-test 2>/dev/null || true
            ctr task delete -f io-test 2>/dev/null || true
            ctr container delete io-test 2>/dev/null || true
        }
        cleanup
        ctr container create --runtime io.containerd.mica.v2 ${IMAGE_NAME} io-test
        (echo 'uname'; sleep 2; echo '\004'; sleep 1) | timeout 10 ctr task start io-test 2>&1 | head -20
        cleanup
    " 2>&1)

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    if echo "$output" | grep -qi "UniProton\|Zephyr\|LiteOS"; then
        record_result "ctr foreground mode" "PASS" "${duration}" "uname command output received"
        log_pass "ctr foreground works"
        return 0
    else
        record_result "ctr foreground mode" "FAIL" "${duration}" "No expected output. Output: ${output}"
        log_fail "ctr foreground failed"
        return 1
    fi
}

# ============================================
# Test 3: nerdctl TTY mode (-it)
# ============================================
test_nerdctl_tty() {
    local start_time=$(date +%s)

    log_test "Test 3: nerdctl TTY mode (nerdctl run -it)"

    local output=$(run_remote "
        cleanup() {
            nerdctl kill io-test 2>/dev/null || true
            nerdctl rm -f io-test 2>/dev/null || true
        }
        cleanup
        timeout 10 sh -c '(echo help; sleep 2) | nerdctl run -it --rm --runtime io.containerd.mica.v2 ${IMAGE_NAME}' 2>&1 | head -20
        cleanup
    " 2>&1)

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    if echo "$output" | grep -q "support shell commond"; then
        record_result "nerdctl TTY mode (-it)" "PASS" "${duration}" "help command output received"
        log_pass "nerdctl TTY mode works"
        return 0
    else
        record_result "nerdctl TTY mode (-it)" "FAIL" "${duration}" "No expected output. Output: ${output}"
        log_fail "nerdctl TTY mode failed"
        return 1
    fi
}

# ============================================
# Test 4: nerdctl non-TTY mode (-i)
# ============================================
test_nerdctl_notty() {
    local start_time=$(date +%s)

    log_test "Test 4: nerdctl non-TTY mode (nerdctl run -i)"

    local output=$(run_remote "
        cleanup() {
            nerdctl kill io-test 2>/dev/null || true
            nerdctl rm -f io-test 2>/dev/null || true
        }
        cleanup
        timeout 10 sh -c '(echo help; sleep 2) | nerdctl run -i --rm --runtime io.containerd.mica.v2 ${IMAGE_NAME}' 2>&1 | head -20
        cleanup
    " 2>&1)

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    if echo "$output" | grep -q "support shell commond"; then
        record_result "nerdctl non-TTY mode (-i)" "PASS" "${duration}" "help command output received"
        log_pass "nerdctl non-TTY mode works"
        return 0
    else
        record_result "nerdctl non-TTY mode (-i)" "FAIL" "${duration}" "No expected output. Output: ${output}"
        log_fail "nerdctl non-TTY mode failed"
        return 1
    fi
}

# ============================================
# Test 5: Multiple command execution
# ============================================
test_multiple_commands() {
    local start_time=$(date +%s)

    log_test "Test 5: Multiple command execution (help, uname, vendor)"

    local output=$(run_remote "
        cleanup() {
            ctr task kill -s 9 io-test 2>/dev/null || true
            ctr task delete -f io-test 2>/dev/null || true
            ctr container delete io-test 2>/dev/null || true
        }
        cleanup
        ctr container create --runtime io.containerd.mica.v2 ${IMAGE_NAME} io-test
        ctr task start -d io-test
        sleep 2
        (echo 'help'; echo 'uname'; echo 'exit'; sleep 2) | timeout 10 ctr task attach io-test 2>&1
        cleanup
    " 2>&1)

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    local cmd_count=$(echo "$output" | grep -c "support shell commond\|UniProton\|openEuler" || true)

    if [ "$cmd_count" -ge 2 ]; then
        record_result "Multiple command execution" "PASS" "${duration}" "Multiple commands executed successfully"
        log_pass "Multiple commands work"
        return 0
    else
        record_result "Multiple command execution" "FAIL" "${duration}" "Not all commands executed. Output: ${output}"
        log_fail "Multiple commands failed"
        return 1
    fi
}

# ============================================
# Test 6: Exit command detection
# ============================================
test_exit_command() {
    local start_time=$(date +%s)

    log_test "Test 6: Exit command detection in TTY mode"

    local output=$(run_remote "
        cleanup() {
            ctr task kill -s 9 io-test 2>/dev/null || true
            ctr task delete -f io-test 2>/dev/null || true
            ctr container delete io-test 2>/dev/null || true
        }
        cleanup
        ctr container create --runtime io.containerd.mica.v2 ${IMAGE_NAME} io-test
        ctr task start -d io-test
        sleep 2
        (echo 'help'; echo 'exit'; sleep 2) | timeout 10 ctr task attach io-test 2>&1 || true
        sleep 1
        ctr task ls | grep -q io-test && echo 'RUNNING' || echo 'STOPPED'
        cleanup
    " 2>&1)

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    if echo "$output" | grep -q "STOPPED"; then
        record_result "Exit command detection" "PASS" "${duration}" "Container stopped on exit command"
        log_pass "Exit command works"
        return 0
    else
        record_result "Exit command detection" "FAIL" "${duration}" "Container did not stop. Output: ${output}"
        log_fail "Exit command failed"
        return 1
    fi
}

# ============================================
# Test 7: Log cleanliness (no high-frequency logs)
# ============================================
test_log_cleanliness() {
    local start_time=$(date +%s)

    log_test "Test 7: Log cleanliness (no high-frequency spam)"

    local log_count=$(run_remote "
        cleanup() {
            ctr task kill -s 9 io-test 2>/dev/null || true
            ctr task delete -f io-test 2>/dev/null || true
            ctr container delete io-test 2>/dev/null || true
        }
        cleanup
        ctr container create --runtime io.containerd.mica.v2 ${IMAGE_NAME} io-test
        ctr task start -d io-test
        sleep 2
        (echo 'help'; sleep 2) | timeout 10 ctr task attach io-test >/dev/null 2>&1 || true
        sleep 1
        tail -100 /var/log/mica/mica-runtime.log | grep -c 'stdin FIFO read' || 0
        cleanup
    " 2>&1)

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    if [ "$log_count" -lt 5 ]; then
        record_result "Log cleanliness" "PASS" "${duration}" "Only ${log_count} high-frequency logs (good)"
        log_pass "Logs are clean"
        return 0
    else
        record_result "Log cleanliness" "FAIL" "${duration}" "Too many logs: ${log_count}"
        log_fail "Logs have too much spam"
        return 1
    fi
}

# ============================================
# Main test runner
# ============================================
main() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              MicRun IO Comprehensive Test Suite                       ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""
    log_info "Remote host: ${REMOTE_HOST}"
    log_info "Test image: ${IMAGE_NAME}"
    log_info "Test timeout: ${TEST_TIMEOUT}s"
    echo ""

    # Check remote connectivity
    if ! run_remote "echo 'connected'" >/dev/null 2>&1; then
        log_fail "Cannot connect to remote host ${REMOTE_HOST}"
        exit 1
    fi
    log_info "Connected to remote host"
    echo ""

    # Run all tests
    test_ctr_background_attach
    test_ctr_foreground
    test_nerdctl_tty
    test_nerdctl_notty
    test_multiple_commands
    test_exit_command
    test_log_cleanliness

    # Print results table
    print_results_table

    # Return exit code based on failures
    local failed=0
    for r in "${TEST_RESULTS[@]}"; do
        [ "$r" = "FAIL" ] && ((failed++))
    done

    if [ $failed -eq 0 ]; then
        echo -e "${GREEN}All tests passed!${NC}"
    else
        echo -e "${RED}${failed} test(s) failed!${NC}"
    fi

    exit $failed
}

# Run main
main "$@"
