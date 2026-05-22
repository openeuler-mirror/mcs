#!/bin/bash

# Enhanced containerd and shim management with improved reliability
# Adds service validation, crash recovery, and resource cleanup verification

if [ -n "${MICRUN_TEST_CONTAINERD_ENHANCED_SH_LOADED:-}" ]; then
    return 0
fi
MICRUN_TEST_CONTAINERD_ENHANCED_SH_LOADED=1

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${COMMON_DIR}/env.sh"
source "${COMMON_DIR}/remote.sh"
source "${COMMON_DIR}/assert.sh"

# Enhanced configuration
CONTAINERD_STARTUP_TIMEOUT="${CONTAINERD_STARTUP_TIMEOUT:-30}"
CONTAINERD_RESTART_MAX="${CONTAINERD_RESTART_MAX:-3}"
CONTAINERD_SOCKET_PATH="${CONTAINERD_SOCKET_PATH:-/run/containerd/containerd.sock}"
SHIM_BINARY_PATH="${SHIM_BINARY_PATH:-/usr/bin/containerd-shim-mica-v2}"
SHIM_CRASH_DETECTION_ENABLED="${SHIM_CRASH_DETECTION_ENABLED:-true}"

# Logging
log_containerd_debug() {
    if [ "${CONTAINERD_DEBUG:-false}" = "true" ]; then
        log_info "[CONTAINERD-DEBUG] $1"
    fi
}

log_containerd_warn() {
    log_warn "[CONTAINERD] $1"
}

log_containerd_error() {
    log_error "[CONTAINERD] $1"
}

# Check containerd service status with detailed output
containerd_check_status() {
    local status_output
    local active_state
    local sub_state

    status_output=$(remote "
        systemctl show containerd --property=LoadState,ActiveState,SubState,MainPID 2>/dev/null || echo 'failed'
    " 2>&1)

    if echo "$status_output" | grep -q "failed"; then
        log_containerd_error "Failed to query containerd status"
        return 1
    fi

    active_state=$(echo "$status_output" | grep "^ActiveState=" | cut -d= -f2)
    sub_state=$(echo "$status_output" | grep "^SubState=" | cut -d= -f2)

    log_containerd_debug "Containerd state: $active_state / $sub_state"

    if [ "$active_state" = "active" ] && [ "$sub_state" = "running" ]; then
        return 0
    fi

    return 1
}

# Wait for containerd socket to be ready
containerd_wait_socket() {
    local timeout="${1:-$CONTAINERD_STARTUP_TIMEOUT}"
    local elapsed=0
    local interval=2

    log_containerd_debug "Waiting for containerd socket (timeout: ${timeout}s)"

    while [ "$elapsed" -lt "$timeout" ]; do
        if remote "test -S '$CONTAINERD_SOCKET_PATH'"; then
            # Try to connect to the socket
            if remote "ctr version >/dev/null 2>&1"; then
                log_containerd_debug "Containerd socket ready (${elapsed}s)"
                return 0
            fi
        fi

        sleep "$interval"
        elapsed=$((elapsed + interval))
    done

    log_containerd_error "Containerd socket not ready after ${timeout}s"
    return 1
}

# Enhanced containerd startup with validation
containerd_ensure_enhanced() {
    local attempt=1
    local max_attempts="$CONTAINERD_RESTART_MAX"

    while [ "$attempt" -le "$max_attempts" ]; do
        log_containerd_debug "Ensuring containerd (attempt $attempt/$max_attempts)"

        # Check if containerd is already running
        if containerd_check_status; then
            if containerd_wait_socket 10; then
                log_containerd_debug "Containerd already running and healthy"
                return 0
            fi
        fi

        # Try to start containerd
        log_containerd_debug "Starting containerd..."
        remote "systemctl restart containerd" >/dev/null 2>&1 || true

        # Wait for socket
        if containerd_wait_socket "$CONTAINERD_STARTUP_TIMEOUT"; then
            log_success "Containerd started successfully"
            return 0
        fi

        log_containerd_warn "Failed to start containerd (attempt $attempt/$max_attempts)"

        if [ "$attempt" -lt "$max_attempts" ]; then
            # Attempt cleanup before retry
            containerd_force_cleanup
            sleep 2
        fi

        attempt=$((attempt + 1))
    done

    log_containerd_error "Failed to start containerd after $max_attempts attempts"
    return 1
}

# Force cleanup of containerd resources
containerd_force_cleanup() {
    log_containerd_debug "Forcing containerd cleanup..."

    remote "
        # Kill shim processes
        pkill -9 containerd-shim-mica-v2 2>/dev/null || true
        pkill -9 containerd-shim 2>/dev/null || true

        # Remove runtime state
        rm -rf /run/containerd/io.containerd.runtime.v2.task/default/* 2>/dev/null || true

        # Remove socket if stale
        if [ -S '$CONTAINERD_SOCKET_PATH' ]; then
            rm -f '$CONTAINERD_SOCKET_PATH' 2>/dev/null || true
        fi
    " >/dev/null 2>&1 || true

    log_containerd_debug "Force cleanup completed"
}

# Verify containerd cleanup
containerd_verify_cleanup() {
    local remaining_shims
    local remaining_tasks
    local remaining_containers

    log_containerd_debug "Verifying containerd cleanup..."

    remaining_shims=$(remote "
        ps aux | grep -c '[c]ontainerd-shim-mica-v2' || echo '0'
    " 2>/dev/null | tr -d '\n ')

    remaining_tasks=$(remote "
        ctr task ls -q 2>/dev/null | wc -l || echo '0'
    " 2>/dev/null | tr -d '\n ')

    remaining_containers=$(remote "
        ctr container ls -q 2>/dev/null | wc -l || echo '0'
    " 2>/dev/null | tr -d '\n ')

    log_containerd_debug "Remaining - shims: $remaining_shims, tasks: $remaining_tasks, containers: $remaining_containers"

    if [ "${remaining_shims:-0}" -gt 0 ]; then
        log_containerd_warn "Found $remaining_shims remaining shim processes"
        return 1
    fi

    if [ "${remaining_tasks:-0}" -gt 0 ]; then
        log_containerd_warn "Found $remaining_tasks remaining tasks"
        return 1
    fi

    if [ "${remaining_containers:-0}" -gt 0 ]; then
        # Note: Some containers may be intentionally kept
        log_containerd_debug "Found $remaining_containers remaining containers (may be intentional)"
    fi

    log_containerd_debug "Cleanup verification passed"
    return 0
}

# Enhanced cleanup with verification
containerd_cleanup_enhanced() {
    local max_attempts=3
    local attempt=1

    log_containerd_debug "Starting enhanced cleanup..."

    while [ "$attempt" -le "$max_attempts" ]; do
        log_containerd_debug "Cleanup attempt $attempt/$max_attempts"

        # Standard cleanup
        remote "
            # Kill all tasks
            for task in \$(ctr task ls -q 2>/dev/null); do
                ctr task kill -s 9 \"\$task\" 2>/dev/null || true
            done

            # Delete all tasks
            for task in \$(ctr task ls -q 2>/dev/null); do
                ctr task delete \"\$task\" 2>/dev/null || true
            done

            # Delete all containers
            for container in \$(ctr container ls -q 2>/dev/null); do
                ctr container delete \"\$container\" 2>/dev/null || true
            done

            # Kill shim processes
            pkill -9 containerd-shim-mica-v2 2>/dev/null || true
        " >/dev/null 2>&1 || true

        # Kill xen domains
        remote "
            for domain in \$(xl list 2>/dev/null | awk '{print \$1}' | grep -v '^Name\$' | grep -v '^Domain-0\$'); do
                xl destroy \"\$domain\" 2>/dev/null || true
            done
        " >/dev/null 2>&1 || true

        # Restart containerd
        remote "systemctl restart containerd" >/dev/null 2>&1 || true

        sleep 3

        # Verify cleanup
        if containerd_verify_cleanup; then
            log_success "Containerd cleanup verified"
            return 0
        fi

        log_containerd_warn "Cleanup verification failed, retrying..."
        attempt=$((attempt + 1))
    done

    log_containerd_error "Failed to verify cleanup after $max_attempts attempts"
    return 1
}

# Shim crash detection and recovery
containerd_detect_shim_crash() {
    if [ "$SHIM_CRASH_DETECTION_ENABLED" != "true" ]; then
        return 1
    fi

    local shim_count
    local shim_status

    shim_count=$(remote "
        ps aux | grep -c '[c]ontainerd-shim-mica-v2' || echo '0'
    " 2>/dev/null | tr -d '\n ')

    shim_status=$(remote "
        if [ -x '$SHIM_BINARY_PATH' ]; then
            '$SHIM_BINARY_PATH' --version 2>&1 | head -1
        else
            echo 'not_found'
        fi
    " 2>/dev/null)

    log_containerd_debug "Shim count: $shim_count, status: $shim_status"

    # Check if shim binary exists
    if echo "$shim_status" | grep -q "not_found"; then
        log_containerd_error "Shim binary not found: $SHIM_BINARY_PATH"
        return 0  # Indicate crash detected
    fi

    return 1  # No crash
}

# Recover from shim crash
containerd_recover_shim() {
    log_containerd_warn "Attempting shim recovery..."

    # Cleanup crashed shim resources
    remote "
        pkill -9 containerd-shim-mica-v2 2>/dev/null || true
        rm -rf /run/containerd/io.containerd.runtime.v2.task/default/* 2>/dev/null || true
    " >/dev/null 2>&1 || true

    # Restart containerd
    if containerd_ensure_enhanced; then
        log_success "Shim recovery completed"
        return 0
    fi

    log_containerd_error "Shim recovery failed"
    return 1
}

# Check shim version compatibility
containerd_check_shim_version() {
    local shim_version
    local required_version="${SHIM_REQUIRED_VERSION:-}"

    if [ -z "$required_version" ]; then
        log_containerd_debug "No required shim version specified, skipping check"
        return 0
    fi

    shim_version=$(remote "
        if [ -x '$SHIM_BINARY_PATH' ]; then
            '$SHIM_BINARY_PATH' --version 2>&1 | grep -oP '(?<=version )[^[:space:]]+' || echo 'unknown'
        else
            echo 'not_found'
        fi
    " 2>/dev/null | tr -d '\n ')

    log_containerd_debug "Shim version: $shim_version (required: $required_version)"

    if [ "$shim_version" = "not_found" ]; then
        log_containerd_error "Shim binary not found: $SHIM_BINARY_PATH"
        return 1
    fi

    if [ "$shim_version" = "unknown" ]; then
        log_containerd_warn "Could not determine shim version"
        return 0  # Don't fail on unknown version
    fi

    # Simple version comparison (assumes semantic versioning)
    if printf '%s\n%s\n' "$required_version" "$shim_version" | sort -V -C; then
        log_containerd_debug "Shim version compatible"
        return 0
    else
        log_containerd_error "Shim version $shim_version < required $required_version"
        return 1
    fi
}

# Get containerd diagnostics
containerd_get_diagnostics() {
    local diagnostics_file="${1:-/tmp/containerd-diagnostics.txt}"

    {
        echo "=== Containerd Diagnostics ==="
        echo "Timestamp: $(date)"
        echo ""
        echo "=== Service Status ==="
        remote "systemctl status containerd --no-pager" 2>/dev/null || echo "Cannot get status"
        echo ""
        echo "=== Socket Status ==="
        remote "ls -la '$CONTAINERD_SOCKET_PATH' 2>/dev/null; ctr version 2>&1" || echo "Socket not accessible"
        echo ""
        echo "=== Shim Processes ==="
        remote "ps aux | grep '[c]ontainerd-shim'" 2>/dev/null || echo "No shim processes"
        echo ""
        echo "=== Tasks ==="
        remote "ctr task ls" 2>/dev/null || echo "Cannot list tasks"
        echo ""
        echo "=== Containers ==="
        remote "ctr container ls" 2>/dev/null || echo "Cannot list containers"
        echo ""
        echo "=== System Resources ==="
        remote "free -h; df -h /" 2>/dev/null || echo "Cannot get system info"
        echo ""
        echo "=== Recent Logs ==="
        remote "journalctl -u containerd --no-pager -n 50" 2>/dev/null || echo "Cannot get logs"
        echo ""
    } > "$diagnostics_file" 2>&1

    log_info "Containerd diagnostics saved to $diagnostics_file"
}

# Export enhanced functions
export containerd_check_status
export containerd_wait_socket
export containerd_ensure_enhanced
export containerd_force_cleanup
export containerd_verify_cleanup
export containerd_cleanup_enhanced
export containerd_detect_shim_crash
export containerd_recover_shim
export containerd_check_shim_version
export containerd_get_diagnostics
