#!/bin/bash

# Enhanced timing control for IO tests - FIXED VERSION
# Implements dynamic waiting, adaptive timeouts, and state-based synchronization

if [ -n "${MICRUN_TEST_TIMING_ENHANCED_SH_LOADED:-}" ]; then
    return 0
fi
MICRUN_TEST_TIMING_ENHANCED_SH_LOADED=1

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${COMMON_DIR}/env.sh"
source "${COMMON_DIR}/remote.sh"
source "${COMMON_DIR}/assert.sh"

# Enhanced timing configuration
TIMING_CONTAINER_STARTUP_MIN="${TIMING_CONTAINER_STARTUP_MIN:-3}"
TIMING_CONTAINER_STARTUP_MAX="${TIMING_CONTAINER_STARTUP_MAX:-30}"
TIMING_CONTAINER_STARTUP_CHECK_INTERVAL="${TIMING_CONTAINER_STARTUP_CHECK_INTERVAL:-1}"
TIMING_ATTACH_DELAY_BASE="${TIMING_ATTACH_DELAY_BASE:-5}"
TIMING_ATTACH_DELAY_MULTIPLIER="${TIMING_ATTACH_DELAY_MULTIPLIER:-1.5}"
TIMING_IO_RESPONSE_TIMEOUT="${TIMING_IO_RESPONSE_TIMEOUT:-10}"
TIMING_STABILIZATION_WAIT="${TIMING_STABILIZATION_WAIT:-2}"

# Adaptive timeout configuration
TIMEOUT_QUICK="${TIMEOUT_QUICK:-5}"
TIMEOUT_NORMAL="${TIMEOUT_NORMAL:-15}"
TIMEOUT_LONG="${TIMEOUT_LONG:-30}"
TIMEOUT_EXTENDED="${TIMEOUT_EXTENDED:-60}"

# Statistics
TIMING_TOTAL_WAITS=0
TIMING_TOTAL_DELAY=0
TIMING_ADAPTIVE_ADJUSTMENTS=0

# Logging
log_timing_debug() {
    if [ "${TIMING_DEBUG:-false}" = "true" ]; then
        log_info "[TIMING-DEBUG] $1"
    fi
}

# Wait for container to reach RUNNING state with dynamic timeout
wait_container_running() {
    local container_id="$1"
    local max_wait="${2:-$TIMING_CONTAINER_STARTUP_MAX}"
    local check_interval="${3:-$TIMING_CONTAINER_STARTUP_CHECK_INTERVAL}"
    local elapsed=0

    log_timing_debug "Waiting for container $container_id to reach RUNNING state (max: ${max_wait}s)"

    while [ "$elapsed" -lt "$max_wait" ]; do
        if remote "ctr task ls | grep -qE '${container_id}.*RUNNING'"; then
            log_timing_debug "Container $container_id reached RUNNING in ${elapsed}s"
            TIMING_TOTAL_WAITS=$((TIMING_TOTAL_WAITS + 1))
            TIMING_TOTAL_DELAY=$((TIMING_TOTAL_DELAY + elapsed))
            return 0
        fi

        sleep "$check_interval"
        elapsed=$((elapsed + check_interval))
    done

    log_error "Container $container_id did not reach RUNNING within ${max_wait}s"
    return 1
}

# Wait for container task to be created
wait_container_task_created() {
    local container_id="$1"
    local max_wait="${2:-15}"
    local elapsed=0

    log_timing_debug "Waiting for task creation for $container_id"

    while [ "$elapsed" -lt "$max_wait" ]; do
        if remote "ctr task ls | grep -q '$container_id'"; then
            log_timing_debug "Task created for $container_id (${elapsed}s)"
            return 0
        fi

        sleep 1
        elapsed=$((elapsed + 1))
    done

    log_error "Task not created for $container_id within ${max_wait}s"
    return 1
}

# Calculate adaptive attach delay based on container startup time
calculate_adaptive_delay() {
    local startup_time="$1"
    local base_delay="${2:-$TIMING_ATTACH_DELAY_BASE}"
    local multiplier="${3:-$TIMING_ATTACH_DELAY_MULTIPLIER}"
    local adaptive_delay

    # Calculate delay: base + (startup_time * multiplier)
    adaptive_delay=$(awk "BEGIN {printf \"%.0f\", $base_delay + ($startup_time * $multiplier)}")

    # Clamp to reasonable bounds
    if [ "$adaptive_delay" -lt "$base_delay" ]; then
        adaptive_delay="$base_delay"
    elif [ "$adaptive_delay" -gt 30 ]; then
        adaptive_delay=30
    fi

    log_timing_debug "Adaptive delay: ${adaptive_delay}s (startup: ${startup_time}s)"
    echo "$adaptive_delay"
}

# Execute attach with adaptive timing
attach_with_timing() {
    local container_id="$1"
    local startup_time="${2:-$TIMING_CONTAINER_STARTUP_MIN}"
    local input_delay="${3:-}"
    local attach_timeout

    # Calculate adaptive delay if not provided
    if [ -z "$input_delay" ]; then
        input_delay=$(calculate_adaptive_delay "$startup_time")
    fi

    # Set attach timeout based on input delay
    attach_timeout=$((input_delay + TIMEOUT_NORMAL))

    log_timing_debug "Attach timing: input_delay=${input_delay}s, timeout=${attach_timeout}s"

    # Execute attach with timing
    local attach_cmd
    attach_cmd="(sleep $input_delay; printf '\nhelp\n'; sleep $TIMEOUT_IO_RESPONSE_TIMEOUT) | timeout $attach_timeout ctr task attach $container_id 2>&1 || true"

    remote "$attach_cmd"
}

# Wait for IO response with timeout
wait_for_io_response() {
    local container_id="$1"
    local expected_pattern="${2:-support shell}"
    local max_wait="${3:-$TIMING_IO_RESPONSE_TIMEOUT}"
    local elapsed=0

    log_timing_debug "Waiting for IO response matching: $expected_pattern"

    while [ "$elapsed" -lt "$max_wait" ]; do
        if remote "ctr task attach $container_id 2>&1 | head -20" | grep -q "$expected_pattern"; then
            log_timing_debug "IO response detected in ${elapsed}s"
            return 0
        fi

        sleep 1
        elapsed=$((elapsed + 1))
    done

    log_timing_debug "IO response not detected within ${max_wait}s"
    return 1
}

# Adaptive timeout calculation based on historical performance
calculate_adaptive_timeout() {
    local operation_type="$1"
    local historical_avg="${2:-}"
    local base_timeout

    case "$operation_type" in
        "quick")
            base_timeout="$TIMEOUT_QUICK"
            ;;
        "normal")
            base_timeout="$TIMEOUT_NORMAL"
            ;;
        "long")
            base_timeout="$TIMEOUT_LONG"
            ;;
        "extended")
            base_timeout="$TIMEOUT_EXTENDED"
            ;;
        *)
            base_timeout="$TIMEOUT_NORMAL"
            ;;
    esac

    # Use historical average if available and within reasonable bounds
    if [ -n "$historical_avg" ] && [ "$historical_avg" -gt 0 ]; then
        local adjusted_timeout
        adjusted_timeout=$(awk "BEGIN {printf \"%.0f\", $historical_avg * 1.5}")

        if [ "$adjusted_timeout" -ge "$base_timeout" ] && [ "$adjusted_timeout" -le "$TIMEOUT_EXTENDED" ]; then
            log_timing_debug "Using historical average: ${adjusted_timeout}s"
            echo "$adjusted_timeout"
            return 0
        fi
    fi

    echo "$base_timeout"
}

# Wait for resource stabilization between tests
wait_for_stabilization() {
    local wait_time="${1:-$TIMING_STABILIZATION_WAIT}"
    local checks="${2:-process,network,disk}"

    log_timing_debug "Waiting for resource stabilization (${wait_time}s)..."

    sleep "$wait_time"

    # Perform stability checks
    if [[ "$checks" == *"process"* ]]; then
        if ! remote "ps aux | grep -q '[c]ontainerd'"; then
            log_warn "Containerd process not found during stability check"
        fi
    fi

    if [[ "$checks" == *"network"* ]]; then
        if ! remote "ctr version >/dev/null 2>&1"; then
            log_warn "Containerd not responding during stability check"
        fi
    fi

    log_timing_debug "Stabilization complete"
}

# State-based synchronization instead of fixed sleep
sync_to_state() {
    local state_check_command="$1"
    local max_wait="${2:-30}"
    local check_interval="${3:-1}"
    local elapsed=0

    log_timing_debug "Syncing to state: $state_check_command"

    while [ "$elapsed" -lt "$max_wait" ]; do
        if eval "$state_check_command" >/dev/null 2>&1; then
            log_timing_debug "State reached in ${elapsed}s"
            return 0
        fi

        sleep "$check_interval"
        elapsed=$((elapsed + check_interval))
    done

    log_error "State not reached within ${max_wait}s: $state_check_command"
    return 1
}

# Measure and record operation timing
measure_timing() {
    local operation_name="$1"
    shift
    local start_time
    local end_time
    local duration
    local result

    start_time=$(date +%s)

    result=$("$@" 2>&1)
    local exit_code=$?

    end_time=$(date +%s)
    duration=$((end_time - start_time))

    log_timing_debug "Operation ${operation_name} took ${duration}s exit: ${exit_code}"

    # Record timing for adaptive calculations
    if [ -f "/tmp/micrun-timing-stats.txt" ]; then
        echo "${operation_name}:${duration}" >> "/tmp/micrun-timing-stats.txt"
    fi

    printf '%s\n' "$result"
    return "$exit_code"
}

# Enhanced container creation with timing
container_create_with_timing() {
    local image="$1"
    local container_id="$2"
    local create_timeout="${3:-$TIMEOUT_NORMAL}"

    log_timing_debug "Creating container $container_id with ${create_timeout}s timeout"

    local start_time
    start_time=$(date +%s)

    if ! remote_with_timeout "$create_timeout" "ctr container create --runtime io.containerd.mica.v2 $image $container_id >/dev/null 2>&1"; then
        log_error "Container creation failed"
        return 1
    fi

    local end_time
    end_time=$(date +%s)
    local create_duration=$((end_time - start_time))

    log_timing_debug "Container created in ${create_duration}s"

    # Return creation time for adaptive calculations
    echo "$create_duration"
    return 0
}

# Enhanced container start with timing
container_start_with_timing() {
    local container_id="$1"
    local max_wait="${2:-$TIMING_CONTAINER_STARTUP_MAX}"

    log_timing_debug "Starting container $container_id"

    local start_time
    start_time=$(date +%s)

    # Start task in background
    if ! remote "ctr task start -d $container_id >/dev/null 2>&1"; then
        log_error "Task start failed"
        return 1
    fi

    # Wait for RUNNING state
    if ! wait_container_running "$container_id" "$max_wait"; then
        log_error "Container did not reach RUNNING state"
        return 1
    fi

    local end_time
    end_time=$(date +%s)
    local startup_duration=$((end_time - start_time))

    log_timing_debug "Container started in ${startup_duration}s"

    # Return startup time for adaptive calculations
    echo "$startup_duration"
    return 0
}

# Get timing statistics
get_timing_stats() {
    cat <<EOF
Timing Statistics:
  Total waits: $TIMING_TOTAL_WAITS
  Total delay: ${TIMING_TOTAL_DELAY}s
  Adaptive adjustments: $TIMING_ADAPTIVE_ADJUSTMENTS
EOF

    if [ -f "/tmp/micrun-timing-stats.txt" ]; then
        echo ""
        echo "Operation History:"
        awk -F: '{
            op[$1]++; sum[$1] += $2; count[$1]++
        }
        END {
            for (op in op) {
                avg = sum[op] / count[op]
                printf "  %s: avg %.1fs (%d operations)\n", op, avg, count[op]
            }
        }' /tmp/micrun-timing-stats.txt
    fi
}

# Export enhanced timing functions
export wait_container_running
export wait_container_task_created
export calculate_adaptive_delay
export attach_with_timing
export wait_for_io_response
export calculate_adaptive_timeout
export wait_for_stabilization
export sync_to_state
export measure_timing
export container_create_with_timing
export container_start_with_timing
export get_timing_stats
export TIMING_TOTAL_WAITS
export TIMING_TOTAL_DELAY
export TIMING_ADAPTIVE_ADJUSTMENTS
