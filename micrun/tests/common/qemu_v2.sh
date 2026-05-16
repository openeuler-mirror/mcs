#!/bin/bash

# Enhanced QEMU testing utilities - Simplified fixed version
# Focus on core functionality without complex quoting

if [ -n "${MICRUN_TEST_QEMU_ENHANCED_V2_SH_LOADED:-}" ]; then
    return 0
fi
MICRUN_TEST_QEMU_ENHANCED_V2_SH_LOADED=1

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${COMMON_DIR}/qemu.sh"
source "${COMMON_DIR}/assert.sh"

# Configuration
QEMU_SSH_MAX_RETRIES="${QEMU_SSH_MAX_RETRIES:-30}"
QEMU_SSH_INITIAL_BACKOFF="${QEMU_SSH_INITIAL_BACKOFF:-1}"
QEMU_SSH_MAX_BACKOFF="${QEMU_SSH_MAX_BACKOFF:-10}"

# Logging
log_qemu_debug() {
    if [ "${QEMU_DEBUG:-false}" = "true" ]; then
        log_info "[QEMU-DEBUG] $1"
    fi
}

# Calculate exponential backoff
qemu_calc_backoff() {
    local attempt="$1"
    local initial="$QEMU_SSH_INITIAL_BACKOFF"
    local max="$QEMU_SSH_MAX_BACKOFF"
    local backoff

    backoff=$((initial * (1 << (attempt - 1))))
    if [ "$backoff" -gt "$max" ]; then
        backoff="$max"
    fi

    printf '%d\n' "$backoff"
}

# Enhanced SSH wait with exponential backoff
qemu_wait_for_ssh_v2() {
    local max_retries="${1:-$QEMU_SSH_MAX_RETRIES}"
    local attempt=1
    local backoff

    log_qemu_debug "Waiting for SSH with exponential backoff (max: $max_retries)"

    qemu_forget_forwarded_hostkey

    while [ "$attempt" -le "$max_retries" ]; do
        backoff=$(qemu_calc_backoff "$attempt")
        log_qemu_debug "Attempt $attempt/$max_retries (backoff: ${backoff}s)"

        if qemu_local_ssh "echo ok" >/dev/null 2>&1; then
            log_qemu_debug "SSH connected"
            return 0
        fi

        if [ "$attempt" -lt "$max_retries" ]; then
            sleep "$backoff"
        fi

        attempt=$((attempt + 1))
    done

    log_error "SSH connection failed after $max_retries attempts"
    return 1
}

# Simple health check
qemu_health_check_v2() {
    log_qemu_debug "Performing health check"

    # Check SSH connection
    if ! qemu_local_ssh "echo ok" >/dev/null 2>&1; then
        log_error "SSH health check failed"
        return 1
    fi

    # Check containerd
    if ! qemu_local_ssh "systemctl is-active containerd" >/dev/null 2>&1; then
        log_warn "Containerd not active"
    fi

    log_qemu_debug "Health check passed"
    return 0
}

# Enhanced cleanup with verification
qemu_cleanup_v2() {
    log_qemu_debug "Starting cleanup"

    qemu_stop_existing

    sleep 2

    # Verify no QEMU processes remain
    local remaining
    remaining=$(ps aux | grep '[q]emu-system-aarch64' | wc -l) || true

    if [ "${remaining:-0}" -gt 0 ]; then
        log_warn "Found $remaining remaining QEMU processes"
        return 1
    fi

    log_qemu_debug "Cleanup verified"
    return 0
}

# Get diagnostics
qemu_get_diagnostics_v2() {
    local diagnostics_file="${1:-/tmp/qemu-diagnostics.txt}"

    {
        echo "=== QEMU Diagnostics ==="
        echo "Timestamp: $(date)"
        echo ""
        echo "=== SSH Test ==="
        if qemu_local_ssh "echo SSH OK" 2>&1; then
            echo "SSH: OK"
        else
            echo "SSH: FAILED"
        fi
        echo ""
        echo "=== Containerd Status ==="
        qemu_local_ssh "systemctl is-active containerd" 2>/dev/null || echo "Cannot check"
        echo ""
    } > "$diagnostics_file" 2>&1

    log_info "Diagnostics saved to $diagnostics_file"
    cat "$diagnostics_file"
}

# Export functions
export qemu_wait_for_ssh_v2
export qemu_health_check_v2
export qemu_cleanup_v2
export qemu_get_diagnostics_v2
