#!/bin/bash

if [ -n "${MICRUN_TEST_REMOTE_SH_LOADED:-}" ]; then
    return 0
fi
MICRUN_TEST_REMOTE_SH_LOADED=1

remote_ssh_opts() {
    local opts=(
        "-o" "StrictHostKeyChecking=no" \
        "-o" "UserKnownHostsFile=/dev/null" \
        "-o" "GlobalKnownHostsFile=/dev/null" \
        "-o" "LogLevel=ERROR" \
        "-o" "ConnectTimeout=${TEST_SSH_CONNECT_TIMEOUT:-10}"
    )

    if [ -n "${TEST_REMOTE_PORT:-}" ]; then
        opts+=("-p" "${TEST_REMOTE_PORT}")
    fi

    printf '%s\n' "${opts[@]}"
}

remote_scp_opts() {
    local opts=(
        "-o" "StrictHostKeyChecking=no" \
        "-o" "UserKnownHostsFile=/dev/null" \
        "-o" "GlobalKnownHostsFile=/dev/null" \
        "-o" "LogLevel=ERROR" \
        "-o" "ConnectTimeout=${TEST_SSH_CONNECT_TIMEOUT:-10}"
    )

    if [ -n "${TEST_REMOTE_PORT:-}" ]; then
        opts+=("-P" "${TEST_REMOTE_PORT}")
    fi

    printf '%s\n' "${opts[@]}"
}

_remote_ssh() {
    local host="$1"
    shift

    if [ -n "${TEST_REMOTE_PASSWORD:-}" ]; then
        # shellcheck disable=SC2046
        sshpass -p "$TEST_REMOTE_PASSWORD" ssh $(remote_ssh_opts) "$host" "$@"
    else
        # shellcheck disable=SC2046
        ssh $(remote_ssh_opts) "$host" "$@"
    fi
}

remote() {
    local host command

    if [ "$#" -eq 1 ]; then
        host="${TEST_REMOTE_HOST}"
        command="$1"
    else
        host="${1:-$TEST_REMOTE_HOST}"
        command="$2"
    fi

    _remote_ssh "$host" "$command" 2>/dev/null
}

remote_output() {
    local host command

    if [ "$#" -eq 1 ]; then
        host="${TEST_REMOTE_HOST}"
        command="$1"
    else
        host="${1:-$TEST_REMOTE_HOST}"
        command="$2"
    fi

    _remote_ssh "$host" "$command" 2>&1
}

remote_can_connect() {
    local host="${1:-$TEST_REMOTE_HOST}"

    _remote_ssh "$host" "echo ok" >/dev/null 2>&1
}

copy_to_remote() {
    local src="$1"
    local target="${2:-$TEST_REMOTE_HOST}"
    local dst="$3"

    if [ -n "${TEST_REMOTE_PASSWORD:-}" ]; then
        # shellcheck disable=SC2046
        sshpass -p "$TEST_REMOTE_PASSWORD" scp $(remote_scp_opts) "$src" "$target:$dst" >/dev/null
    else
        # shellcheck disable=SC2046
        scp $(remote_scp_opts) "$src" "$target:$dst" >/dev/null
    fi
}

copy_from_remote() {
    local src="$1"
    local target="${2:-$TEST_REMOTE_HOST}"
    local dst="$3"

    if [ -n "${TEST_REMOTE_PASSWORD:-}" ]; then
        # shellcheck disable=SC2046
        sshpass -p "$TEST_REMOTE_PASSWORD" scp $(remote_scp_opts) "$target:$src" "$dst" >/dev/null
    else
        # shellcheck disable=SC2046
        scp $(remote_scp_opts) "$target:$src" "$dst" >/dev/null
    fi
}

forget_known_host() {
    local host_port="$1"
    ssh-keygen -R "$host_port" >/dev/null 2>&1 || true
}
