#!/bin/bash

# Never trigger GUI askpass prompts; fail fast instead (see remote.sh).
unset SSH_ASKPASS SUDO_ASKPASS
export SSH_ASKPASS_REQUIRE=never

# Enhanced remote execution - Simplified version
# Focus on core retry functionality without complex quoting

if [ -n "${MICRUN_TEST_REMOTE_ENHANCED_V2_SH_LOADED:-}" ]; then
    return 0
fi
MICRUN_TEST_REMOTE_ENHANCED_V2_SH_LOADED=1

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${COMMON_DIR}/remote.sh"

# Configuration
REMOTE_RETRY_MAX="${REMOTE_RETRY_MAX:-3}"
REMOTE_RETRY_INITIAL_BACKOFF="${REMOTE_RETRY_INITIAL_BACKOFF:-1}"
REMOTE_RETRY_MAX_BACKOFF="${REMOTE_RETRY_MAX_BACKOFF:-8}"

# Calculate backoff
remote_calc_backoff() {
    local attempt="$1"
    local initial="$REMOTE_RETRY_INITIAL_BACKOFF"
    local max="$REMOTE_RETRY_MAX_BACKOFF"
    local backoff

    backoff=$((initial * (1 << (attempt - 1))))
    if [ "$backoff" -gt "$max" ]; then
        backoff="$max"
    fi

    printf '%d\n' "$backoff"
}

# Enhanced remote command with retry
remote_retry_v2() {
    local command="$1"
    local max_retries="${2:-$REMOTE_RETRY_MAX}"
    local attempt=1
    local backoff

    while [ "$attempt" -le "$max_retries" ]; do
        backoff=$(remote_calc_backoff "$attempt")

        if [ "$attempt" -gt 1 ]; then
            log_info "Retry $attempt/$max_retries (backoff: ${backoff}s)"
            sleep "$backoff"
        fi

        # Capture the real exit code: a failed `if cond; then ... fi`
        # without an else branch yields status 0, which used to make the
        # transient-error case below unreachable and every failure report
        # "exit: 0".
        remote "$command" 2>&1
        local exit_code=$?
        if [ "$exit_code" -eq 0 ]; then
            if [ "$attempt" -gt 1 ]; then
                log_success "Command succeeded on retry $attempt"
            fi
            return 0
        fi

        # Check if error is transient
        case "$exit_code" in
            255|124)
                # Transient error - retry
                log_warn "Transient error (exit: $exit_code), will retry"
                ;;
            *)
                # Non-transient error
                log_error "Non-retryable error (exit: $exit_code)"
                return 1
                ;;
        esac

        attempt=$((attempt + 1))
    done

    log_error "Command failed after $max_retries attempts"
    return 1
}

# Safe file transfer with retry
copy_to_remote_safe_v2() {
    local src="$1"
    local dst="$2"
    local max_retries=3
    local attempt=1

    while [ "$attempt" -le "$max_retries" ]; do
        # copy_to_remote takes (src, host, dst); the old call passed an
        # undefined $REMOTE as src and shifted every argument.
        if copy_to_remote "$src" "$TEST_REMOTE_HOST" "$dst" 2>/dev/null; then
            if remote "test -f '$dst'"; then
                return 0
            fi
            log_warn "File transfer verification failed (attempt $attempt/$max_retries)"
        fi

        if [ "$attempt" -lt "$max_retries" ]; then
            sleep 2
        fi
        attempt=$((attempt + 1))
    done

    log_error "Failed to transfer file after $max_retries attempts"
    return 1
}

# Get retry statistics
remote_get_stats_v2() {
    echo "Remote execution statistics available"
}

# Export functions
export remote_retry_v2
export copy_to_remote_safe_v2
export remote_get_stats_v2
