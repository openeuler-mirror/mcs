#!/bin/bash
# UX Tests Runner
# 运行所有用户体验测试脚本

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REMOTE_HOST="root@192.168.7.2"
TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="$TEST_DIR/ux_results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Create results directory
mkdir -p "$RESULTS_DIR"

# Test summary
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_WARNED=0

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1" | tee -a "$RESULTS_DIR/summary_$TIMESTAMP.log"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" | tee -a "$RESULTS_DIR/summary_$TIMESTAMP.log"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" | tee -a "$RESULTS_DIR/summary_$TIMESTAMP.log"
}

log_test() {
    echo -e "${BLUE}[TEST]${NC} $1" | tee -a "$RESULTS_DIR/summary_$TIMESTAMP.log"
}

# Header
echo ""
echo "=========================================="
echo "MicRun UX Tests"
echo "=========================================="
echo "Remote host: $REMOTE_HOST"
echo "Results dir: $RESULTS_DIR"
echo "Timestamp: $TIMESTAMP"
echo ""
echo "Tests will be run in the following order:"
echo "  1. test_ux_echo.exp - Echo/Duplicate Output Detection"
echo "  2. test_ux_consistency.exp - Command Consistency Verification"
echo "  3. test_ux_prompt.exp - Prompt Behavior Verification"
echo ""

# Check expect is available
if ! command -v expect &> /dev/null; then
    log_error "expect is not installed. Please install expect:"
    echo "  - Ubuntu/Debian: apt-get install expect"
    echo "  - RHEL/CentOS: yum install expect"
    exit 1
fi

# Cleanup function
cleanup() {
    log_info "Cleaning up test containers..."
    ssh -o BatchMode=yes "$REMOTE_HOST" "
        for c in test-ux-echo test-ux-consistency test-ux-prompt; do
            ctr task kill -s SIGKILL \$c 2>/dev/null || true
            ctr task delete -f \$c 2>/dev/null || true
            ctr container delete \$c 2>/dev/null || true
        done
    " 2>/dev/null
}

# Cleanup before starting
cleanup

# Run a single test
run_test() {
    local test_name="$1"
    local test_file="$TEST_DIR/$test_name"

    TESTS_RUN=$((TESTS_RUN + 1))
    log_test "Running $test_name..."

    local output_file="$RESULTS_DIR/${test_name%.exp}_$TIMESTAMP.log"

    if expect "$test_file" > "$output_file" 2>&1; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        log_info "✓ $test_name PASSED"
    else
        # Check for warnings in output
        if grep -q "WARN:" "$output_file" 2>/dev/null; then
            if ! grep -q "FAIL:" "$output_file" 2>/dev/null; then
                # Only warnings, no failures
                TESTS_WARNED=$((TESTS_WARNED + 1))
                log_warn "⚠ $test_name WARNINGS (see $output_file)"
            else
                # Has failures
                TESTS_FAILED=$((TESTS_FAILED + 1))
                log_error "✗ $name FAILED (see $output_file)"
            fi
        else
            TESTS_FAILED=$((TESTS_FAILED + 1))
            log_error "✗ $test_name FAILED (see $output_file)"
        fi
    fi

    # Copy detail log if exists
    local detail_file="$TEST_DIR/${test_name%.exp}_detail.log"
    if [ -f "$detail_file" ]; then
        cp "$detail_file" "$RESULTS_DIR/${test_name%.exp}_detail_$TIMESTAMP.log"
        rm -f "$detail_file"
    fi
}

# Run all tests
log_info "Starting UX tests..."
echo ""

run_test "test_ux_echo.exp"
echo ""
run_test "test_ux_consistency.exp"
echo ""
run_test "test_ux_prompt.exp"

# Cleanup after tests
cleanup

# Print summary
echo ""
echo "=========================================="
echo "Test Summary"
echo "=========================================="
echo "Tests run:    $TESTS_RUN"
echo -e "Passed:       ${GREEN}$TESTS_PASSED${NC}"
echo -e "Warnings:     ${YELLOW}$TESTS_WARNED${NC}"
echo -e "Failed:       ${RED}$TESTS_FAILED${NC}"
echo ""

if [ $TESTS_FAILED -eq 0 ] && [ $TESTS_WARNED -eq 0 ]; then
    log_info "All tests passed!"
    exit 0
elif [ $TESTS_FAILED -eq 0 ]; then
    log_warn "Some tests had warnings"
    exit 0
else
    log_error "Some tests failed"
    exit 1
fi
