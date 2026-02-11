#!/bin/bash
# Stress test for MicRun IO system
# Tests high-frequency attach/detach and continuous output

set -e

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test configuration
CONTAINER_NAME="stress-test-$$"
IMAGE_NAME="localhost:5000/mica-uniproton-app:xen-0.1"
ITERATIONS=50
LOG_FILE="/tmp/io_stress_test.log"

# Log functions
log_info() {
	echo -e "${GREEN}[INFO]${NC} $1" | tee -a "$LOG_FILE"
}

log_warn() {
	echo -e "${YELLOW}[WARN]${NC} $1" | tee -a "$LOG_FILE"
}

log_error() {
	echo -e "${RED}[ERROR]${NC} $1" | tee -a "$LOG_FILE"
}

log_test() {
	echo -e "${YELLOW}[TEST]${NC} $1" | tee -a "$LOG_FILE"
}

# Cleanup function
cleanup() {
	log_info "Cleaning up..."
	ctr task kill -s 9 ${CONTAINER_NAME} 2>/dev/null || true
	sleep 1
	ctr task delete -f ${CONTAINER_NAME} 2>/dev/null || true
	ctr container delete ${CONTAINER_NAME} 2>/dev/null || true
}

# Trap to ensure cleanup on exit
trap cleanup EXIT

# Check if container exists
container_exists() {
	ctr container ls | grep -q "${CONTAINER_NAME}"
}

# Check if task is running
task_running() {
	ctr task ls | grep -q "${CONTAINER_NAME}"
}

# Test 1: High-frequency attach/detach
test_attach_detach_stress() {
	log_test "Test 1: High-frequency attach/detach stress test (${ITERATIONS} iterations)"

	# Create container
	log_info "Creating container..."
	ctr container create ${IMAGE_NAME} ${CONTAINER_NAME}

	if ! container_exists; then
		log_error "Failed to create container"
		return 1
	fi

	# Start container in background
	log_info "Starting container in background..."
	timeout 180 ctr task start ${CONTAINER_NAME} </dev/null >/dev/null 2>&1 &
	TASK_PID=$!

	# Wait for RTOS to boot
	log_info "Waiting for RTOS to boot (60s)..."
	sleep 60

	if ! task_running; then
		log_error "Container task not running after start"
		return 1
	fi

	# Perform multiple attach/detach cycles
	log_info "Starting attach/detach stress test..."
	success=0
	failed=0

	for i in $(seq 1 ${ITERATIONS}); do
		# Attach and immediately detach (send Ctrl+P Ctrl+Q)
		echo -e "\x10\x11" | timeout 3 ctr task attach ${CONTAINER_NAME} >/dev/null 2>&1 && {
			((success++))
		} || {
			((failed++))
		}

		if [ $((i % 10)) -eq 0 ]; then
			log_info "Progress: $i/${ITERATIONS} (success: $success, failed: $failed)"
		fi

		# Small delay between iterations
		sleep 0.1
	done

	log_info "Attach/detach stress test completed:"
	log_info "  Total iterations: ${ITERATIONS}"
	log_info "  Successful: ${success}"
	log_info "  Failed: ${failed}"

	if [ $failed -gt 0 ]; then
		log_warn "Warning: $failed iterations failed out of ${ITERATIONS}"
	fi

	# Kill background task
	kill $TASK_PID 2>/dev/null || true
	wait $TASK_PID 2>/dev/null || true

	# Cleanup for next test
	cleanup
	sleep 2

	return 0
}

# Test 2: Continuous output during attach
test_continuous_output() {
	log_test "Test 2: Continuous output with multiple attaches"

	# Create and start container
	log_info "Creating container..."
	ctr container create ${IMAGE_NAME} ${CONTAINER_NAME}

	log_info "Starting container..."
	timeout 180 ctr task start ${CONTAINER_NAME} </dev/null >/dev/null 2>&1 &
	TASK_PID=$!

	# Wait for RTOS to boot
	log_info "Waiting for RTOS to boot (60s)..."
	sleep 60

	# Attach, send command, detach
	log_info "Testing command output with attach/detach..."
	for i in $(seq 1 10); do
		# Send help command and detach
		(echo "help"; sleep 2; echo -e "\x11\x11") | timeout 5 ctr task attach ${CONTAINER_NAME} 2>&1 | grep -q "support shell" && {
			log_info "  Iteration $i: Got expected output"
		} || {
			log_warn "  Iteration $i: Failed to get expected output"
		}
		sleep 0.5
	done

	# Kill background task
	kill $TASK_PID 2>/dev/null || true
	wait $TASK_PID 2>/dev/null || true

	# Cleanup
	cleanup
	sleep 2

	return 0
}

# Test 3: Memory leak detection
test_memory_leak() {
	log_test "Test 3: Memory leak detection (monitor micrun process)"

	# Get initial memory
	local initial_mem=$(ps aux | grep '[c]ontainerd-shim-mica-v2' | awk '{print $6}' || echo "0")
	log_info "Initial memory usage: ${initial_mem} KB"

	# Create and start container
	log_info "Creating container..."
	ctr container create ${IMAGE_NAME} ${CONTAINER_NAME}

	log_info "Starting container..."
	timeout 180 ctr task start ${CONTAINER_NAME} </dev/null >/dev/null 2>&1 &
	TASK_PID=$!

	# Wait for RTOS to boot
	log_info "Waiting for RTOS to boot (60s)..."
	sleep 60

	# Perform multiple attach/detach cycles
	log_info "Performing attach/detach cycles..."
	for i in $(seq 1 20); do
		echo -e "\x11\x11" | timeout 3 ctr task attach ${CONTAINER_NAME} >/dev/null 2>&1
		sleep 0.2
	done

	# Check memory after cycles
	local final_mem=$(ps aux | grep '[c]ontainerd-shim-mica-v2' | awk '{print $6}' || echo "0")
	log_info "Final memory usage: ${final_mem} KB"

	# Calculate memory growth
	local mem_growth=$((final_mem - initial_mem))
	log_info "Memory growth: ${mem_growth} KB"

	if [ $mem_growth -gt 10000 ]; then
		log_warn "Warning: Memory growth exceeds 10 MB (${mem_growth} KB)"
		log_warn "This may indicate a memory leak"
	else
		log_info "Memory growth is within acceptable range"
	fi

	# Kill background task
	kill $TASK_PID 2>/dev/null || true
	wait $TASK_PID 2>/dev/null || true

	return 0
}

# Main test execution
main() {
	echo "=========================================="
	echo "MicRun IO Stress Test Suite"
	echo "=========================================="
	echo "Log file: $LOG_FILE"
	echo ""

	# Clear log file
	: > "$LOG_FILE"

	# Run tests
	test_attach_detach_stress
	test_continuous_output
	test_memory_leak

	echo ""
	echo "=========================================="
	echo "Stress test completed"
	echo "=========================================="
	echo "Full log: $LOG_FILE"
}

main
